<script lang="ts">
  import { onDestroy, onMount, tick, untrack } from 'svelte';
  import AttachmentProgress from '$components/AttachmentProgress.svelte';
  import Button from '$components/ui/Button.svelte';
  import QuestionForm from '$components/QuestionForm.svelte';
  import {
    MAX_PANE_SIZE_COLUMNS,
    MAX_PANE_SIZE_ROWS,
    MIN_PANE_SIZE_COLUMNS,
    MIN_PANE_SIZE_ROWS,
    paneLeaseRenewalAllowed,
  } from '$lib/config';
  import {
    agentNeedsInspection,
    agentNeedsResponse,
    attentionKind,
    approvalButtonTone,
    approvalOptions,
    questionInteraction,
    sortedAgents,
  } from '$lib/agents';
  import { clearPromptDraft, loadPromptDraft, savePromptDraft } from '$lib/prompt-drafts';
  import {
    findTerminalText,
    terminalMatchFragments,
    terminalRowForOffset,
    terminalRowOffsets,
    terminalSearchText,
  } from '$lib/terminal-find';
  import {
    dictationState,
    dictationSupported,
    startDictation,
    stopDictation,
  } from '$lib/dictation';
  import {
    armSpeechKeepalive,
    releaseSpeechKeepalive,
    speak,
    speechEnabled,
    speechEngine,
    speechLanguage,
    speechLanguageLabel,
    speechState,
    stopSpeech,
  } from '$lib/speech';
  import { interfaceSize, terminalHeightLease, theme } from '$lib/preferences';
  import { cachedPaneSize, storePaneSize, stabilizePaneSize } from '$lib/pane-size-cache';
  import { agentChords } from '$lib/agent-chords';
  import { replaceView } from '$lib/router';
  import { targetRefForAgent, targetRefMatchesAgent } from '$lib/resource-id';
  import { securityState } from '$lib/security';
  import { relayStore } from '$lib/store';
  import {
    latestCompletedResponse,
    renderedRowShift,
    stripAnsi,
    TERMINAL_SEPARATOR_TOKEN,
    renderTerminalContent,
    terminalResizeLayoutEngaged,
    terminalScreenColumns,
  } from '$lib/terminal';
  import { detectTerminalMenu, terminalTextInputMode } from '$lib/terminal-menu';
  import type { AttachmentBatchController, AttachmentBatchSnapshot, AttachmentRef } from '$lib/attachments';
  import type {
    Agent,
    SlashCommand,
    SlashCommandCatalog,
    TerminalFrame,
  } from '$lib/types';
  import type { RenderedTerminalRow } from '$lib/terminal';
  import { VirtualTerminalIndex } from '$lib/virtual-terminal';
  import { mountTerminalWakeLock } from '$lib/wake-lock';

  const connections = relayStore.connections;

  let {
    agent,
    allAgents,
    frame,
    responding,
    readOnly = false,
  }: {
    agent: Agent;
    allAgents: Agent[];
    frame?: TerminalFrame;
    responding: Set<string>;
    readOnly?: boolean;
  } = $props();

  interface VirtualTerminalAnchor {
    index: number;
    offset: number;
    text: string;
    /** true when the anchor came from the DOM scan (its element exists); the
     * indexAt fallback is estimate-only and must never drive a DOM
     * correction — it can point at a mounted row dozens of rows away. */
    dom?: boolean;
    /** Verbatim texts of the anchor row and the rows under it — the
     * fingerprint. A 5–6 line sequence is effectively unique in a
     * scrollback, so resolving by content is immune to index drift,
     * crop shifts, and rowShift false positives (the jump-to-top vector). */
    block?: string[];
  }

  const ANCHOR_BLOCK_ROWS = 6;

  function anchorBlock(index: number): string[] {
    const block: string[] = [];
    for (let cursor = index; cursor < Math.min(renderedRows.length, index + ANCHOR_BLOCK_ROWS); cursor += 1) {
      block.push(renderedRows[cursor].text.trim());
    }
    return block;
  }


  interface QueuedKeyCommand {
    /** 'keys' → send_keys, 'input' → send_input (typed text/semantic keys), 'text' → send_text (raw bytes). */
    kind?: 'keys' | 'input' | 'text';
    keys?: string[];
    text?: string;
    label: string;
    /** Key-row button to flash while this command is in flight. */
    flashId?: string;
    resolve: (sent: boolean) => void;
  }
  let terminalElement = $state<HTMLDivElement>(null!);
  let cellMeasureElement = $state<HTMLSpanElement>(null!);
  let fileInput = $state<HTMLInputElement>(null!);
  let imageInput = $state<HTMLInputElement>(null!);
  let modifierInputElement = $state<HTMLInputElement>(null!);
  let findInputElement = $state<HTMLInputElement>(null!);
  let composerElement = $state<HTMLTextAreaElement>(null!);
  let transcriptElement = $state<HTMLTextAreaElement>(null!);
  let responseElement = $state<HTMLTextAreaElement>(null!);
  let agentResponsePreviewElement = $state<HTMLTextAreaElement>(null!);
  let directInputElement = $state<HTMLTextAreaElement>(null!);
  let copiedAgentResponseText = $state('');
  let composer = $state(untrack(() => loadPromptDraft(agent)));
  let composerFocused = $state(false);
  let sendingPrompt = $state(false);
  // Never handed to savePromptDraft: a no-echo prompt answer must not be
  // written to this phone's storage.
  let secretValue = $state('');
  let sendingSecret = $state(false);
  // Composer text as it stood when a no-echo prompt was first recognized, and
  // whether the user has changed it since. Recognition is a heuristic, so it
  // pauses persistence instead of deleting a draft the prompt did not author.
  let noEchoDraftBaseline: string | null = null;
  let noEchoDraftTainted = false;
  let draftPersistenceWarning = $state('');
  // Dictation: the draft before the mic started is kept so interim fragments
  // render appended to it without committing until recognition finalizes.
  let dictationBase = $state('');
  let historyTruncated = $state(false);
  // Pre-resize frame kept only to suppress display of transient frames while
  // the agent repaints at the new width.
  // Raw state: the effect compares frame identity, which a deep proxy breaks.
  let resizeFrameBaseline = $state.raw<TerminalFrame | undefined>(undefined);
  // The relay flags frames while a pane repaints at a new size. Suppressing
  // them keeps a half-drawn screen off the phone, but a shared session whose
  // desktop client keeps fighting the leased size can flag every frame, so the
  // wait is bounded: a stale screen is worse than a transient one.
  let resizeWaitExpired = $state(false);
  let resizeWaitTimer: ReturnType<typeof setTimeout> | null = null;
  let displayed = $state('');
  let renderedHtml = $state('');
  let renderedRows = $state<RenderedTerminalRow[]>([]);
  let virtualRows = $state<{ index: number; html: string }[]>([]);
  let virtualTopHeight = $state(0);
  let virtualBottomHeight = $state(0);
  let virtualContentColumns = $state(0);
  let virtualStart = 0;
  let virtualEnd = 0;
  let virtualLayoutSignature = '';
  let virtualStickToBottom = true;
  let virtualScrollResetPending = false;
  let pendingResizeAnchor: VirtualTerminalAnchor | null = null;
  let pendingResizeStick: boolean | null = null;
  let pendingLayoutStick: boolean | null = null;
  let virtualScrollTop = 0;
  let virtualScrollHeight = 0;
  let virtualClientHeight = 0;
  let virtualWindowFrame = 0;
  let virtualRowObserver: ResizeObserver | undefined;
  let virtualHeightCache = new Map<string, number>();
  // Heights measured per row index. Unlike the html-keyed cache these survive
  // content churn at an index whose height did not change, so a repaint does
  // not fall back to estimates that re-anchor the viewport off by rows.
  let measuredRowSizes = new Map<number, number>();
  // A programmatic scrollTop write that overshot the content clamps at the
  // bottom; the resulting scroll event must not re-arm stick-to-bottom.
  let suppressBottomPinOnce = false;
  // Finger drags emit a scroll event per sub-pixel step, and classifying each
  // event alone (scrollTop < virtualScrollTop - 1) needs a >1px drop inside
  // ONE event — a slow drag never dropped the pin and the atBottom branch
  // slammed the view back every step. Accumulate upward travel across
  // events; a downward move resets the counter.
  let scrollUpAccum = 0;
  // Any scroll while a resize settle waits means the anchor captured at wait
  // start is stale: the apply that ends the wait must re-anchor live.
  let pendingAnchorStale = false;
  // A finger dragging the log freezes the viewport: while it is down, DOM
  // churn is compensated by a translateY on .term-screen — compositor-level,
  // synchronous, no scroll events, unclampable — instead of scrollTop writes
  // that can paint a wrong position for a frame before the correction lands
  // (the visible "jumps then snaps back"). The accumulated offset folds back
  // into scrollTop on release. Viewport-top positions therefore use
  // docScrollTop() = scrollTop - heldOffset while held.
  let touchHeld = false;
  let heldOffset = 0;
  let termScreenElement = $state<HTMLElement>(null!);
  const virtualIndex = new VirtualTerminalIndex();
  const wideGridOffsets = new Map<number, number>();
  let wideGridOffsetsPane = '';
  let lastFormat = '';
  let lastContent = '';
  let lastPreserveLayout = false;
  let lastPreserveLineEnds = false;
  let lastRenderColumnCap = 0;
  let jumpVisible = $state(false);
  let arrowsOpen = $state(false);
  let fkeysOpen = $state(false);
  let findOpen = $state(false);
  // Wide view: lease the pane at the relay's 240-column cap so ASCII diagrams
  // and wide tables render unwrapped; the terminal scrolls horizontally.
  let wideView = $state(false);
  let findQuery = $state('');
  let activeFindIndex = $state(-1);
  let ctrlArmed = $state(false);
  let shiftArmed = $state(false);
  let altArmed = $state(false);
  // Direct typing mode: a hidden capture field forwards keystrokes straight to
  // the pane (send_input/send_keys/send_text) instead of the buffered composer.
  let directInput = $state(false);
  let directComposing = false;
  let directBackspaceAt = 0;
  let keyFlashId = $state('');
  let keyFlashError = $state(false);
  let keyRequestSending = $state(false);
  let sendingFilter = $state(false);
  const keySending = $derived(keyRequestSending || sendingFilter);
  let uploadStatus = $state('');
  let uploadError = $state(false);
  let uploadingAttachment = $state(false);
  let attachmentController = $state<AttachmentBatchController | null>(null);
  let attachmentSnapshot = $state<AttachmentBatchSnapshot | null>(null);
  let attachmentUnsubscribe: (() => void) | null = null;
  let attachmentCancelRequested = false;
  let copyingAgentResponse = $state(false);
  let paneSizeLeaseError = $state('');
  let requestedPaneId = '';
  let slashCatalog = $state<SlashCommandCatalog>({ commands: [], truncated: false });
  let slashCatalogLoading = $state(true);
  let slashCatalogUnavailable = $state(false);
  let slashCatalogRequest = 0;
  let slashCatalogTarget = '';
  const slashCatalogIdentity = $derived(JSON.stringify([
    agent.relay_id, agent.server_session_id, agent.raw_pane_id,
    agent.terminal_id, agent.generation, agent.agent_session_id, agent.agent, agent.cwd,
  ]));
  let activeSlashIndex = $state(0);
  let dismissedSlashQuery = $state<string | null>(null);
  let dismissedMenuSignature = $state('');
  const CELL_MEASURE_TEXT = '0000000000';
  const PANE_SIZE_LEASE_REFRESH_MS = 10_000;
  const PANE_REALTIME_RESYNC_MS = 15_000;
  const MAX_VISIBLE_SLASH_COMMANDS = 200;
  // One relay poll of slack over its own three-second settling window.
  const PANE_RESIZE_WAIT_MAX_MS = 4_000;
  // Herdr's key parser covers f1..f24; the pad exposes the range phones need.
  const FUNCTION_KEYS = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12];
  const RESPONSE_COPY_AGENT_IDS = new Set([
    'hermes', 'hermesagent',
    'claude', 'claudecode', 'codex', 'openaicodex', 'kimi', 'kimicode',
    'omp', 'ohmypi', 'pi', 'picodingagent', 'qoder', 'qodercli',
  ]);
  function responseCopyProfileSupported(agentName: unknown): boolean {
    const normalized = String(agentName || '').trim().toLocaleLowerCase().replace(/\s+/g, '').replace(/-/g, '');
    return RESPONSE_COPY_AGENT_IDS.has(normalized);
  }
  let componentMounted = false;
  let leaseGeneration = 0;
  let leaseInFlight = false;
  let leaseTarget: Agent | null = null;
  // The first confirmed lease of a mount must always fetch the pane's live
  // screen: with a seeded cache the lease usually applies "unchanged", and for
  // lease-capable relays nothing else performs the initial read — skipping it
  // left the terminal stuck on 'Loading…'.
  let leaseReadDone = false;
  // Seeded from the global cache: the phone's width never changes, so every
  // pane leases the same size. This engages the wrapping layout on the first
  // paint instead of after a measure → lease → settle round trip; the
  // background lease below still confirms it.
  const cachedPaneLease = cachedPaneSize();
  let lastLeasedColumns = $state(cachedPaneLease.columns);
  let lastLeasedRows = cachedPaneLease.rows;
  let renderedResizeColumns = $state(0);
  let measuredCellWidth = $state(0);
  let queuedLease: { columns: number; rows: number; force: boolean } | null = null;
  // Set when the page goes hidden; renewals continue within the grace so a
  // desktop browser that reports occluded windows as hidden does not lapse
  // the lease on every app switch.
  let hiddenAt = 0;
  // A locked app is showing this pane to nobody who has verified, so it counts
  // as hidden for reads and watches. The connection now survives the lock, so
  // without this the terminal would keep streaming behind the unlock screen.
  const paneVisible = () => document.visibilityState === 'visible' && !$securityState.locked;
  // The lease needs its own gate: being locked is not the same as being
  // briefly hidden. The grace window below exists so an app switch does not
  // resize the computer's pane, but an unverified holder must not keep it
  // narrowed at all, so a lock skips the grace instead of entering it. The
  // relay's TTL hands the size back if the lock outlasts it.
  const paneLeaseAllowed = () => !$securityState.locked
    && paneLeaseRenewalAllowed(document.visibilityState === 'visible', hiddenAt, Date.now());
  // A lock flip changes what "visible" means for this pane, and no visibility
  // event comes with it, so the pane handler is called directly.
  let paneVisibilityChanged: (() => void) | null = null;
  let paneLocked = false;
  const keyQueue: QueuedKeyCommand[] = [];
  let keyFlashTimer: ReturnType<typeof setTimeout> | undefined;
  let keyReadTimer: ReturnType<typeof setTimeout> | undefined;

  const responsePending = $derived(agentNeedsResponse(agent));
  const approvalMode = $derived(responsePending && attentionKind(agent) === 'approval');
  const inspectionMode = $derived(agentNeedsInspection(agent));
  const interaction = $derived(questionInteraction(agent));
  const questionMode = $derived(Boolean(!readOnly && responsePending && attentionKind(agent) === 'question' && interaction));
  const resizeSessionActive = $derived(
    Boolean(!readOnly && $connections.get(agent.relay_id)?.capabilities.includes('pane_size_lease')),
  );
  // The capability only says the relay can lease; it does not mean this pane
  // has a width yet. The wrapping layout is engaged solely when it does.
  const resizeLayoutActive = $derived(terminalResizeLayoutEngaged(
    resizeSessionActive,
    measuredCellWidth,
    lastLeasedColumns || renderedResizeColumns,
  ));
  // Three regimes, not two. A relay that cannot lease at all serves a pane at
  // its own width, and the fixed layout keeps that pane's columns aligned and
  // scrolling sideways. A relay that can lease but has not granted a width yet
  // is showing a pane whose width is nobody's: alignment is already lost, so
  // those rows wrap at the container instead of stranding the reader on line
  // tails after every refresh. Readers use the same wrapping regime because
  // their role can never obtain a size lease.
  const resizeLayoutPending = $derived(readOnly || (resizeSessionActive && !resizeLayoutActive));
  const options = $derived(approvalOptions(agent));
  const nextBlocked = $derived(sortedAgents(allAgents.filter((item) => agentNeedsResponse(item) && item.pane_id !== agent.pane_id))[0]);
  const slashQuery = $derived(composer.startsWith('/') && !/\s/.test(composer) ? composer.slice(1).toLocaleLowerCase() : null);
  const matchingSlashCommands = $derived.by(() => {
    if (slashQuery === null) return [];
    if (!slashQuery) return slashCatalog.commands;
    return slashCatalog.commands.filter((entry) => entry.command.slice(1).toLocaleLowerCase().startsWith(slashQuery));
  });
  const filteredSlashCommands = $derived(matchingSlashCommands.slice(0, MAX_VISIBLE_SLASH_COMMANDS));
  const slashMatchesHidden = $derived(matchingSlashCommands.length > filteredSlashCommands.length);
  const effectiveSlashIndex = $derived(filteredSlashCommands.length
    ? Math.min(activeSlashIndex, filteredSlashCommands.length - 1)
    : -1);
  const terminalPlainText = $derived(
    stripAnsi(displayed)
      .replaceAll(TERMINAL_SEPARATOR_TOKEN, '────────'),
  );
  const terminalTextMode = $derived.by(() => {
    const mode = terminalTextInputMode(terminalPlainText);
    if (mode === 'filter' || inspectionMode) return mode;
    return null;
  });
  const inputLocked = $derived(readOnly || responsePending || inspectionMode || terminalTextMode === 'filter');
  const composerLocked = $derived(readOnly || responsePending || (inspectionMode && !terminalTextMode));
  const slashMenuOpen = $derived(!inputLocked
    && !questionMode
    && slashQuery !== null
    && dismissedSlashQuery !== composer);
  // The relay recognizes the prompt; that recognition is what opens the masked
  // input, even while the generic composer stays locked for inspection.
  const noEchoActive = $derived(Boolean(frame?.paneId === agent.pane_id && frame?.noEcho));
  const noEchoPrompt = $derived(noEchoActive ? frame?.noEchoPrompt || 'Password:' : '');
  const secretInputSupported = $derived(
    Boolean($connections.get(agent.relay_id)?.capabilities.includes('secret_input')),
  );
  const secretMode = $derived(!readOnly && noEchoActive && secretInputSupported);
  const terminalMenu = $derived(detectTerminalMenu(terminalPlainText));
  const visibleTerminalMenu = $derived(
    !approvalMode
    && !questionMode
    && terminalMenu
    && terminalMenu.signature !== dismissedMenuSignature
      ? terminalMenu
      : null,
  );
  const terminalFindCorpus = $derived.by(() => ({
    text: terminalSearchText(renderedRows),
    offsets: terminalRowOffsets(renderedRows),
  }));
  const terminalFind = $derived(findTerminalText(terminalFindCorpus.text, findQuery.trim()));
  const agentResponseCopySupported = $derived.by(() => {
    const connection = $connections.get(agent.relay_id);
    return Boolean(
      connection?.capabilities.includes('agent_response_copy')
      && responseCopyProfileSupported(agent.agent),
    );
  });
  const relaySpeechLanguages = $derived($connections.get(agent.relay_id)?.speechLanguages ?? []);
  const terminalCopyText = $derived(latestCompletedResponse(frame?.content || ''));
  const terminalContentStyle = $derived.by(() => {
    // Every width is emitted in px of the measured probe cell, never in ch:
    // Safari's Core Text port resolves 1ch against different font metrics
    // than the rendered glyph advance, so an Nch cap wraps short of N cells.
    // Before the probe is measured no cap is emitted at all. The wrapping
    // layout is gated on the same predicate, so the missing cap can no longer
    // fall back to the container width and shred rows mid-word: the fixed
    // layout applies instead until a real cap exists.
    if (measuredCellWidth <= 0) return undefined;
    const capColumns = lastLeasedColumns || renderedResizeColumns;
    const styles = [`--terminal-cell-width: ${measuredCellWidth.toFixed(4)}px`];
    if (resizeLayoutActive) {
      styles.push(`--terminal-width: ${(capColumns * measuredCellWidth).toFixed(4)}px`);
    }
    if (virtualContentColumns > 0) {
      styles.push(`--terminal-content-width: ${(virtualContentColumns * measuredCellWidth).toFixed(4)}px`);
    }
    return styles.join(';');
  });

  const NOT_PERSISTED_AT_HIDDEN_PROMPT =
    'Not saved on this phone: typed while the terminal was hiding its input.';

  $effect(() => {
    if (noEchoActive) {
      // Persistence pauses so nothing typed at a hidden prompt reaches storage.
      // The stored draft stays: it predates the prompt, and recognition is a
      // heuristic that must not be able to destroy the user's own text.
      if (noEchoDraftBaseline === null) noEchoDraftBaseline = composer;
      if (composer !== noEchoDraftBaseline) noEchoDraftTainted = true;
      draftPersistenceWarning = noEchoDraftTainted ? NOT_PERSISTED_AT_HIDDEN_PROMPT : '';
      return;
    }
    noEchoDraftBaseline = null;
    if (!composer) noEchoDraftTainted = false;
    if (noEchoDraftTainted) {
      // Authored while the prompt was up: it may be the secret in the wrong
      // field, so it stays in memory until the composer is cleared or sent.
      draftPersistenceWarning = NOT_PERSISTED_AT_HIDDEN_PROMPT;
      return;
    }
    const result = savePromptDraft(agent, composer);
    draftPersistenceWarning = result === 'too-large'
      ? 'This draft is too large to persist; it survives pane switches but not closing the app.'
      : result === 'unavailable'
        ? 'Draft persistence is unavailable; keep this page open.'
        : '';
  });

  $effect(() => {
    if (secretMode) return;
    untrack(() => { secretValue = ''; });
  });

  $effect(() => {
    const count = terminalFind.matches.length;
    const query = findQuery.trim();
    untrack(() => {
      if (!query || !count) {
        activeFindIndex = -1;
        return;
      }
      if (activeFindIndex < 0 || activeFindIndex >= count) activeFindIndex = 0;
    });
  });

  $effect(() => {
    const highlightState = [
      findOpen,
      findQuery,
      activeFindIndex,
      virtualRows,
      terminalFind.matches.length,
    ];
    void highlightState;
    void tick().then(applyTerminalFindHighlights);
  });

  // The ANSI palette follows the theme's terminal scheme, so a theme change
  // must repaint the cached frame rather than wait for the next output.
  $effect(() => {
    void $theme;
    untrack(() => {
      if (!lastContent) return;
      const next = frame;
      if (!next || next.paneId !== agent.pane_id) return;
      lastContent = '';
      void applyFrame(next, lastPreserveLayout, lastPreserveLineEnds);
    });
  });

  $effect(() => {
    const next = frame;
    const matchingFrame = next?.paneId === agent.pane_id ? next : undefined;
    const preserve = true;
    // Keyed to the session, not to resizeLayoutActive, on purpose: a relay that
    // can lease will repaint this pane at the phone's width, so trailing padding
    // measured at the desktop width is stale whether or not the width has landed
    // yet. A reader cannot lease at all, so its rows wrap at the phone instead
    // of trailing off the side of a desktop-width grid.
    const preserveLineEnds = !resizeSessionActive && !readOnly;
    // A frame read while the agent repaints at a new width is transient. Keep
    // the phone's last stable frame painted until the new stable frame lands,
    // but never past the deadline: a relay that keeps flagging frames would
    // otherwise freeze this pane on a screen that is minutes old.
    const waitingForResizedFrame = resizeSessionActive
      && !paneSizeLeaseError
      && !resizeWaitExpired
      && (lastLeasedColumns === 0
        || (Boolean(resizeFrameBaseline)
          && (next === resizeFrameBaseline || next?.resizeSettling === true)));
    const cachedFrame = waitingForResizedFrame
      && matchingFrame
      && matchingFrame.resizeSettling !== true
      && (!resizeFrameBaseline || matchingFrame === resizeFrameBaseline)
        ? matchingFrame
        : undefined;
    if (waitingForResizedFrame) {
      untrack(() => {
        if (pendingResizeStick !== null) return;
        pendingResizeStick = virtualStickToBottom;
        pendingAnchorStale = false;
        pendingResizeAnchor = virtualStickToBottom
          ? null
          : currentVirtualAnchor(touchHeld ? docScrollTop() : (terminalElement?.scrollTop || 0));
      });
      if (cachedFrame) {
        historyTruncated = Boolean(cachedFrame.truncated);
        untrack(() => { void applyFrame(cachedFrame, preserve, preserveLineEnds, false) });
        return;
      }
      if (renderedRows.length) return;
    }
    if (!matchingFrame || waitingForResizedFrame) {
      untrack(() => {
        if (!waitingForResizedFrame) {
          pendingResizeStick = null;
          pendingResizeAnchor = null;
          pendingAnchorStale = false;
        }
        // Once anything has painted, a transient gap never blanks the pane:
        // background tab spawns and lease renegotiations swap the frame out
        // for a beat, and replacing real output with a placeholder is what
        // read as flicker and ate the reader's scroll position.
        if (renderedRows.length) return;
        const message = waitingForResizedFrame ? 'Resizing terminal…' : 'Loading…';
        const rendered = renderTerminalContent(message, 'plain');
        displayed = rendered.display;
        renderedHtml = rendered.html;
        renderedRows = rendered.rows;
        resetVirtualRows(Number.POSITIVE_INFINITY);
        lastFormat = '';
        lastContent = '';
        jumpVisible = false;
      });
      return;
    }
    if (resizeSessionActive && lastLeasedColumns > 0) renderedResizeColumns = lastLeasedColumns;
    historyTruncated = Boolean(matchingFrame.truncated);
    untrack(() => { void applyFrame(matchingFrame, preserve, preserveLineEnds) });
  });

  $effect(() => {
    const paneId = agent.pane_id;
    const connected = $connections.get(agent.relay_id)?.status === 'connected';
    if (!connected) {
      requestedPaneId = '';
      return;
    }
    if (resizeSessionActive && !paneSizeLeaseError) return;
    if (paneId === requestedPaneId) return;
    requestedPaneId = paneId;
    relayStore.readPane(agent);
  });

  $effect(() => {
    const locked = $securityState.locked;
    if (locked === paneLocked) return;
    paneLocked = locked;
    paneVisibilityChanged?.();
  });

  $effect(() => {
    const connection = $connections.get(agent.relay_id);
    const interfaceSizeValue = $interfaceSize;
    const paneId = agent.pane_id;
    void interfaceSizeValue;
    if (readOnly) {
      releasePaneSizeLease(componentMounted);
      paneSizeLeaseError = '';
      return;
    }
    if (questionMode) {
      releasePaneSizeLease(componentMounted);
      paneSizeLeaseError = '';
      return;
    }
    if (connection?.status !== 'connected') {
      discardPaneSizeLease();
      paneSizeLeaseError = '';
      return;
    }
    if (!connection.capabilities.includes('pane_size_lease')) {
      discardPaneSizeLease();
      paneSizeLeaseError = 'Resize Session is unavailable on this relay.';
      return;
    }
    if (leaseTarget && leaseTarget.pane_id !== paneId) releasePaneSizeLease(componentMounted);
    paneSizeLeaseError = '';
    void tick().then(() => requestPaneSizeLease(false));
  });
  $effect.pre(() => {
    const interfaceSizeValue = $interfaceSize;
    void interfaceSizeValue;
    if (!terminalElement) return;
    // Stickiness is intent, not geometry. A content collapse clamps scrollTop
    // onto a position that only LOOKS like the bottom — re-deriving the flag
    // here re-armed the pin and the next frame slammed the view back to the
    // end. handleScroll owns the flag; this only snapshots it for the layout
    // change in flight.
    pendingLayoutStick = virtualStickToBottom;
  });

  function rememberVirtualScrollGeometry(element: HTMLElement) {
    virtualScrollTop = element.scrollTop;
    virtualScrollHeight = element.scrollHeight;
    virtualClientHeight = element.clientHeight;
  }

  function resetVirtualScroll(element: HTMLElement, stick: boolean) {
    virtualScrollResetPending = true;
    virtualLayoutSignature = '';
    const entryTop = touchHeld ? docScrollTop() : element.scrollTop;
    const holdAnchor = touchHeld ? currentVirtualAnchor(entryTop) : null;
    // Under a held touch the window must render around the live position, not
    // around the stick target — a bottom-window render leaves the viewport
    // staring at spacer for the whole gesture.
    const nextTop = resetVirtualRows(
      touchHeld || !stick ? entryTop : Number.POSITIVE_INFINITY,
    );
    void tick().then(() => {
      const stillStuck = virtualStickToBottom;
      if (touchHeld) {
        // The finger owns the visible row: correct by the anchor element's
        // real displacement, not the estimate that paints the wrong spot.
        applyHeldAnchor(element, holdAnchor, nextTop - entryTop);
        rememberVirtualScrollGeometry(element);
        virtualScrollResetPending = false;
        return;
      }
      element.scrollTop = stillStuck ? element.scrollHeight : nextTop;
      if (!stillStuck
          && element.scrollHeight - element.scrollTop - element.clientHeight < 1) {
        suppressBottomPinOnce = true;
      }
      rememberVirtualScrollGeometry(element);
      if (stillStuck) virtualStickToBottom = true;
      virtualScrollResetPending = false;
    });
  }


  $effect(() => {
    const element = terminalElement;
    const interfaceSizeValue = $interfaceSize;
    void interfaceSizeValue;
    if (!element || typeof ResizeObserver === 'undefined') return;
    let previousWidth = element.clientWidth;
    let previousHeight = element.clientHeight;
    untrack(() => {
      if (!renderedRows.length) return;
      const stick = virtualStickToBottom;
      resetVirtualScroll(element, stick);
    });
    const observer = new ResizeObserver(() => {
      const nextWidth = element.clientWidth;
      const nextHeight = element.clientHeight;
      const widthChanged = Math.abs(nextWidth - previousWidth) >= 1;
      const heightChanged = Math.abs(nextHeight - previousHeight) >= 1;
      if (renderedRows.length && widthChanged) {
        // Flag only — a position check here re-pins when the user deliberately
        // scrolled up a hair, and a clamped scrollTop looks identical.
        resetVirtualScroll(element, virtualStickToBottom);
      } else if (heightChanged && virtualStickToBottom) {
        jumpToBottom();
      } else {
        scheduleVirtualWindow();
      }
      previousWidth = nextWidth;
      previousHeight = nextHeight;
      requestPaneSizeLease(false);
    });
    observer.observe(element);
    return () => observer.disconnect();
  });

  $effect(() => {
    if (!slashMenuOpen || effectiveSlashIndex < 0) return;
    const optionId = `slash-command-option-${effectiveSlashIndex}`;
    void tick().then(() => document.getElementById(optionId)?.scrollIntoView?.({ block: 'nearest' }));
  });

  $effect(() => {
    const resizeFor = [composer, $interfaceSize];
    void tick().then(() => {
      if (resizeFor[0] === composer && resizeFor[1] === $interfaceSize) resizeComposer();
    });
  });

  async function refreshSlashCommands() {
    const request = ++slashCatalogRequest;
    const identity = slashCatalogIdentity;
    const target = { ...agent };
    slashCatalogLoading = true;
    slashCatalogUnavailable = false;
    try {
      const catalog = await relayStore.loadSlashCommands(target, true);
      if (request !== slashCatalogRequest || identity !== slashCatalogIdentity) return;
      slashCatalog = catalog;
    } catch {
      if (request !== slashCatalogRequest || identity !== slashCatalogIdentity) return;
      slashCatalogUnavailable = true;
    } finally {
      if (request === slashCatalogRequest && identity === slashCatalogIdentity) slashCatalogLoading = false;
    }
  }

  $effect(() => {
    const identity = slashCatalogIdentity;
    const status = $connections.get(agent.relay_id)?.status;
    const open = slashMenuOpen;
    untrack(() => {
      ++slashCatalogRequest;
      if (slashCatalogTarget !== identity) {
        slashCatalog = { commands: [], truncated: false };
        slashCatalogTarget = identity;
      }
      if (!open) return;
      if (status && status !== 'connected') {
        slashCatalogLoading = false;
        slashCatalogUnavailable = true;
        return;
      }
      void refreshSlashCommands();
    });
    return () => { ++slashCatalogRequest; };
  });

  onMount(() => {
    componentMounted = true;
    const stopWakeLock = mountTerminalWakeLock();
    const measurePane = () => requestPaneSizeLease(false);
    const realtimeDeltaEnabled = () => Boolean(
      $connections.get(agent.relay_id)?.capabilities.includes('pane_realtime_delta'),
    );
    let lastRefreshAt = Date.now();
    const visibilityChanged = () => {
      if (!paneVisible()) {
        // Traffic stops while hidden, but lease renewals keep going for a
        // bounded grace: desktop Safari reports an occluded window as hidden,
        // so treating every app switch as departure lapsed the lease and
        // resized the shared pane twice per glance, stranding stale copies of
        // inline agents' status bars in the scrollback. Once the grace runs
        // out the renewals stop and the relay's TTL hands the size back — a
        // page hidden overnight cannot keep the pane narrow.
        hiddenAt = Date.now();
        relayStore.unwatchPane(agent);
        return;
      }
      hiddenAt = 0;
      lastRefreshAt = Date.now();
      relayStore.readPane(agent);
      relayStore.watchPane(agent);
      // Re-lease at once: if the lease lapsed while asleep, the pane is back
      // at the desktop width until this lands.
      requestPaneSizeLease(true);
    };
    paneVisibilityChanged = visibilityChanged;
    const findShortcut = (event: KeyboardEvent) => {
      if (questionMode
        || event.altKey
        || (!event.ctrlKey && !event.metaKey)
        || event.key.toLocaleLowerCase() !== 'f') return;
      event.preventDefault();
      openTerminalFind();
    };
    window.addEventListener('keydown', findShortcut);
    window.addEventListener('resize', measurePane);
    window.visualViewport?.addEventListener('resize', measurePane);
    document.addEventListener('visibilitychange', visibilityChanged);
    const refresh = setInterval(() => {
      if (!paneVisible()) return;
      const refreshInterval = realtimeDeltaEnabled() ? PANE_REALTIME_RESYNC_MS : 3_000;
      if (Date.now() - lastRefreshAt < refreshInterval) return;
      lastRefreshAt = Date.now();
      relayStore.readPane(agent);
    }, 3_000);
    if (paneVisible()) relayStore.watchPane(agent);
    const refreshPaneSizeLease = setInterval(
      () => {
        if (paneLeaseAllowed()) requestPaneSizeLease(true);
      },
      PANE_SIZE_LEASE_REFRESH_MS,
    );
    void tick().then(measurePane);
    return () => {
      componentMounted = false;
      window.removeEventListener('resize', measurePane);
      window.visualViewport?.removeEventListener('resize', measurePane);
      document.removeEventListener('visibilitychange', visibilityChanged);
      paneVisibilityChanged = null;
      window.removeEventListener('keydown', findShortcut);
      clearInterval(refresh);
      clearInterval(refreshPaneSizeLease);
      if (keyFlashTimer) clearTimeout(keyFlashTimer);
      if (keyReadTimer) clearTimeout(keyReadTimer);
      for (const command of keyQueue.splice(0)) command.resolve(false);
      relayStore.unwatchPane(agent);
      releasePaneSizeLease(false);
      virtualRowObserver?.disconnect();
      if (virtualWindowFrame) cancelAnimationFrame(virtualWindowFrame);
      stopWakeLock();
    };
  });

  async function applyFrame(
    next: TerminalFrame,
    preserve = true,
    preserveLineEnds = preserve && !resizeSessionActive && !readOnly,
    // Cached-frame replays during a resize wait must not consume the pending
    // stick/anchor snapshot: it belongs to the apply that ends the wait.
    consumePending = true,
  ) {
    const renderColumnCap = resizeLayoutActive
      ? lastLeasedColumns
      : (resizeLayoutPending ? measuredPaneColumns() || 0 : 0);
    const layoutChanged = preserve !== lastPreserveLayout
      || preserveLineEnds !== lastPreserveLineEnds
      || renderColumnCap !== lastRenderColumnCap;
    if (next.content === lastContent && next.format === lastFormat && !layoutChanged) return;
    const rendered = renderTerminalContent(
      next.content,
      next.format,
      preserve,
      preserveLineEnds,
      // Cap box-drawing rows at the width in force. A fixed-grid row is a run
      // of fixed-width cells with no wrap opportunity between them, so past
      // the cap it must render as plain text that can wrap instead. Zero means
      // "no cap", which only a relay that cannot lease is entitled to.
      renderColumnCap,
    );
    lastContent = next.content;
    if (rendered.display === displayed && rendered.html === renderedHtml
      && next.format === lastFormat && !layoutChanged) return;
    // Trust the flag, not the position: re-deriving "at bottom" from a 48px
    // check resurrects pinning when row re-measurement or content growth makes
    // the browser clamp scrollTop onto a position that merely LOOKS near the
    // bottom — the user's scrolled-up intent must win (jump-to-bottom bug).
    const frameStick = virtualStickToBottom;
    // Pending values capture scroll intent when a resize session opens and are
    // consumed only by the apply that ends the wait — a cached-frame replay
    // must not consume them, or the snapshot taken before the user scrolled
    // keeps overriding the live position on every repaint (the slam vector).
    const stick = consumePending && resizeSessionActive && pendingResizeStick !== null
      ? pendingResizeStick
      : consumePending && layoutChanged && pendingLayoutStick !== null
        ? pendingLayoutStick
        : frameStick;
    const previousTop = touchHeld ? docScrollTop() : (terminalElement?.scrollTop || 0);
    let previousAnchor = stick
      ? null
      : consumePending && pendingResizeAnchor && !pendingAnchorStale
        ? pendingResizeAnchor
        : currentVirtualAnchor(previousTop);
    // Rows cropped from the front shift every index; keep the anchor on the
    // same row.
    const rowShift = renderedRowShift(renderedRows, rendered.rows);
    if (rowShift) wideGridOffsets.clear();
    if (rowShift) {
      // Index-keyed heights move with their rows; entries cropped from the
      // front are dropped and the survivors re-indexed.
      const shifted = new Map<number, number>();
      for (const [index, size] of measuredRowSizes) {
        if (index >= rowShift) shifted.set(index - rowShift, size);
      }
      measuredRowSizes = shifted;
    }
    if (previousAnchor && rowShift) {
      previousAnchor = { ...previousAnchor, index: Math.max(0, previousAnchor.index - rowShift) };
    }
    if (consumePending) {
      pendingResizeStick = null;
      pendingResizeAnchor = null;
      pendingAnchorStale = false;
      if (layoutChanged) pendingLayoutStick = null;
    }
    virtualStickToBottom = stick;
    virtualScrollResetPending = Boolean(terminalElement);
    displayed = rendered.display;
    renderedHtml = rendered.html;
    renderedRows = rendered.rows;
    lastFormat = next.format;
    lastPreserveLayout = preserve;
    lastPreserveLineEnds = preserveLineEnds;
    lastRenderColumnCap = renderColumnCap;
    // Under a held touch the window renders around the live position even
    // when pinned: mounting the tail window while the finger holds the
    // viewport mid-list leaves the screen on bare spacer until release.
    const nextTop = resetVirtualRows(
      stick && !touchHeld ? Number.POSITIVE_INFINITY : previousTop,
      previousAnchor,
    );
    await tick();
    if (!terminalElement) {
      virtualScrollResetPending = false;
      return;
    }
    if (layoutChanged) terminalElement.scrollLeft = 0;
    if (touchHeld) {
      // The finger owns the visible row — window around the doc-space
      // position, then fold the measured displacement into the transform.
      renderVirtualWindow(docScrollTop());
      applyHeldAnchor(terminalElement, previousAnchor, nextTop - previousTop);
    } else if (stick) {
      terminalElement.scrollTop = terminalElement.scrollHeight;
      jumpVisible = false;
      virtualStickToBottom = true;
    } else {
      terminalElement.scrollTop = nextTop;
      // A corrected position past the new bottom clamps to it; the scroll
      // event that fires then reads atBottom and would resurrect the pin,
      // dragging the reader to the end on the next frame.
      if (terminalElement.scrollHeight - terminalElement.scrollTop
          - terminalElement.clientHeight < 1) {
        suppressBottomPinOnce = true;
      }
      jumpVisible = true;
    }
    rememberVirtualScrollGeometry(terminalElement);
    virtualScrollResetPending = false;
    observeVirtualRows();
  }

  // The scroll offset in CONTENT coordinates. While a finger is held, part of
  // the displacement lives in the .term-screen transform (heldOffset), so the
  // viewport's position inside the document is scrollTop - heldOffset.
  function docScrollTop(): number {
    return (terminalElement?.scrollTop || 0) - heldOffset;
  }

  function setHeldOffset(offset: number) {
    heldOffset = offset;
    if (termScreenElement) {
      termScreenElement.style.transform = offset ? `translateY(${offset}px)` : '';
    }
  }

  // Fold the transform back into scrollTop — same visual position — then let
  // normal scroll bookkeeping re-derive the pin from real geometry.
  function releaseHeldScroll() {
    if (!terminalElement) {
      heldOffset = 0;
      if (termScreenElement) termScreenElement.style.transform = '';
      return;
    }
    if (heldOffset) {
      const docTop = docScrollTop();
      // Sync the remembered position first so the write's scroll event sees
      // delta 0 — it is a coordinate conversion, not reader movement.
      virtualScrollTop = docTop;
      setHeldOffset(0);
      terminalElement.scrollTop = docTop;
      rememberVirtualScrollGeometry(terminalElement);
    } else if (termScreenElement?.style.transform) {
      termScreenElement.style.transform = '';
    }
  }

  // Under a held touch NOTHING may write scrollTop: any write races the paint
  // and shows up as a jump. DOM churn shifts content at constant scrollTop,
  // so freeze it with the transform instead — correcting the anchor row's
  // measured displacement via heldOffset is synchronous, compositor-cheap,
  // and never fires a scroll event.
  function applyHeldAnchor(
    element: HTMLElement,
    anchor: VirtualTerminalAnchor | null,
    estimateDelta: number,
  ) {
    if (anchor) {
      // Resolve by content fingerprint first: after a render the anchor's
      // captured index may sit in the OLD index space, so it must never
      // drive the lookup directly. The dom flag only gates whether a DOM
      // correction may run at all — the indexAt estimate never reaches in.
      const resolved = matchingAnchorBlock(anchor);
      const index = resolved ?? (anchor.dom ? anchor.index : -1);
      const row = index >= 0
        ? element.querySelector<HTMLElement>(`[data-terminal-row="${index}"]`)
        : null;
      if (row) {
        const desired = element.getBoundingClientRect().top - Math.max(0, anchor.offset);
        setHeldOffset(heldOffset + row.getBoundingClientRect().top - desired);
        return;
      }
    }
    if (Math.abs(estimateDelta) < 0.5) return;
    setHeldOffset(heldOffset - estimateDelta);
  }

  function terminalScreenOffset(): number {
    return terminalElement?.querySelector<HTMLElement>('.term-screen')?.offsetTop || 0;
  }

  function currentVirtualAnchor(scrollTop: number): VirtualTerminalAnchor | null {
    if (!Number.isFinite(scrollTop) || !virtualIndex.length) return null;
    if (terminalElement) {
      const viewport = terminalElement.getBoundingClientRect();
      const element = [...terminalElement.querySelectorAll<HTMLElement>('[data-terminal-row]')]
        .find((row) => {
          const bounds = row.getBoundingClientRect();
          return bounds.bottom > viewport.top && bounds.top < viewport.bottom;
        });
      const index = Number.parseInt(element?.dataset.terminalRow || '', 10);
      if (element && Number.isInteger(index)) {
        return {
          index,
          offset: Math.max(0, viewport.top - element.getBoundingClientRect().top),
          text: renderedRows[index]?.text || '',
          dom: true,
          block: anchorBlock(index),
        };
      }
    }
    const contentTop = terminalScreenOffset();
    const index = virtualIndex.indexAt(Math.max(0, scrollTop - contentTop));
    return {
      index,
      offset: Math.max(0, scrollTop - contentTop - virtualIndex.offset(index)),
      text: renderedRows[index]?.text || '',
      block: anchorBlock(index),
    };
  }

  // Resolve the anchor by CONTENT, not index: a run of five or six verbatim
  // row texts is a near-unique fingerprint in a scrollback, so a crop, a
  // rowShift false positive, or any index-space rewrite cannot re-anchor on
  // a look-alike row — the mechanism that kept snapping the view to the
  // very top when a new frame arrived mid-read.
  function matchingAnchorBlock(anchor: VirtualTerminalAnchor): number | null {
    const block = anchor.block;
    if (!block?.length || !renderedRows.length) return null;
    const needed = Math.max(2, Math.ceil(block.length * 0.6));
    const scoreAt = (start: number): number => {
      let score = 0;
      for (let cursor = 0; cursor < block.length; cursor += 1) {
        const row = renderedRows[start + cursor];
        if (!row || row.text.trim() !== block[cursor]) break;
        score += 1;
      }
      return score;
    };
    // The anchor index rarely moves between frames — try it first.
    if (anchor.index >= 0 && scoreAt(anchor.index) >= needed) return anchor.index;
    // Otherwise scan the whole render: the block is unique enough that a
    // full scan is safe, and only a whole scan survives a large crop.
    let bestIndex = -1;
    let bestScore = 0;
    for (let index = 0; index < renderedRows.length; index += 1) {
      const score = scoreAt(index);
      if (score > bestScore) {
        bestScore = score;
        bestIndex = index;
        if (score === block.length) break;
      }
    }
    return bestScore >= needed ? bestIndex : null;
  }

  // null = no trustworthy match: the caller holds the current scrollTop rather
  // than snapping to the anchor's stale index (that snap is what threw the
  // viewport to the top when the anchor row was a blank or a redrawn line).
  // The anchor's own index is tried first because a frame rarely moves the
  // row under the viewport; when it moved, only a bounded neighborhood is
  // searched — TUIs repeat separator and status rows verbatim, and a
  // whole-list scan happily re-anchors on a look-alike hundreds of rows away,
  // which is the random-jump vector.
  function matchingAnchorIndex(anchor: VirtualTerminalAnchor): number | null {
    const target = anchor.text.trim();
    if (target.length < 4) return null;
    const scoreAt = (index: number): number => {
      if (index < 0 || index >= renderedRows.length) return 0;
      const candidate = renderedRows[index].text.trim();
      return candidate === target
        ? 2
        : target.length >= 8 && (candidate.includes(target) || target.includes(candidate)) ? 1 : 0;
    };
    // The anchor index is already rowShift-corrected by the caller; a verbatim
    // match there is the row itself, no ambiguity.
    if (scoreAt(anchor.index) === 2) return anchor.index;
    const WINDOW = 40;
    const start = Math.max(0, anchor.index - WINDOW);
    const end = Math.min(renderedRows.length - 1, anchor.index + WINDOW);
    let bestIndex = -1;
    let bestScore = 0;
    let bestDistance = Number.POSITIVE_INFINITY;
    for (let index = start; index <= end; index += 1) {
      const score = scoreAt(index);
      if (!score) continue;
      const distance = Math.abs(index - anchor.index);
      if (score > bestScore || (score === bestScore && distance < bestDistance)) {
        bestIndex = index;
        bestScore = score;
        bestDistance = distance;
      }
    }
    return bestIndex >= 0 ? bestIndex : null;
  }

  function anchorOffsetLimit(anchor: VirtualTerminalAnchor, anchorIndex: number): number {
    const target = anchor.text.trim();
    let height = 0;
    let matches = 0;
    let lastSize = 0;
    for (let index = anchorIndex; index < renderedRows.length; index += 1) {
      const candidate = renderedRows[index].text.trim();
      if (!candidate || !target.includes(candidate)) break;
      lastSize = virtualIndex.size(index);
      height += lastSize;
      matches += 1;
      if (candidate === target) break;
    }
    return Math.max(0, matches > 1 ? height - lastSize : height - 1);
  }

  function resetVirtualRows(
    scrollTop: number,
    previousAnchor = currentVirtualAnchor(scrollTop),
  ) {
    const width = terminalElement?.clientWidth
      || Math.round(window.visualViewport?.width || window.innerWidth);
    const layoutSignature = [
      lastPreserveLayout ? 'preserve' : 'readable',
      resizeLayoutActive ? 'resize' : 'fixed',
      lastPreserveLineEnds ? 'line-ends' : 'wrapped',
      lastLeasedColumns || renderedResizeColumns,
      $interfaceSize,
      width,
    ].join(':');
    if (layoutSignature !== virtualLayoutSignature) {
      virtualLayoutSignature = layoutSignature;
      virtualHeightCache.clear();
      measuredRowSizes.clear();
    } else if (virtualHeightCache.size > Math.max(2_000, renderedRows.length * 2)) {
      virtualHeightCache.clear();
    }

    const style = terminalElement ? getComputedStyle(terminalElement) : null;
    const parsedLineHeight = Number.parseFloat(style?.lineHeight || '');
    const lineHeight = Number.isFinite(parsedLineHeight) && parsedLineHeight > 0 ? parsedLineHeight : 18;
    const wrappingColumns = measuredPaneColumns()
      || lastLeasedColumns
      || renderedResizeColumns
      || 80;
    const sizes = renderedRows.map((row, index) => {
      const measuredHere = measuredRowSizes.get(index);
      if (measuredHere) return measuredHere;
      const measured = virtualHeightCache.get(row.html);
      if (measured) return measured;
      if (row.separator) return lineHeight * 1.2;
      // One line per row only where the row really stays on one line: a leased
      // pane's fixed-grid rows. Everything else wraps at the width in force,
      // and estimating those at one line parks the scroll past the content.
      // Rows wrap in the two regimes that have a width to wrap at: engaged
      // (the leased width, fixed grids excepted) and pending (the container).
      // A relay that cannot lease keeps every row on one line.
      const wraps = (!lastPreserveLayout
        || (resizeLayoutPending && !row.wideGrid)
        || (resizeLayoutActive && !row.fixedGrid && !row.wideGrid))
        ? Math.max(1, Math.ceil(row.columns / wrappingColumns))
        : 1;
      return lineHeight * wraps;
    });
    virtualIndex.reset(sizes);
    let nextTop = scrollTop;
    // Under a held touch the anchor math still runs: callers apply only the
    // delta over the live scrollTop, so nextTop must stay anchor-corrected —
    // skipping it leaves content sliding under the finger with no correction.
    if (previousAnchor && virtualIndex.length) {
      // Content fingerprint first — a crop or a rowShift false positive can
      // make the captured index point at the wrong row, and matching it by
      // index is how the viewport snapped to the top. The single-row match
      // stays as the fallback for anchors without a usable block.
      const anchorIndex = matchingAnchorBlock(previousAnchor)
        ?? matchingAnchorIndex(previousAnchor);
      if (anchorIndex !== null) {
        const anchorOffset = Math.min(
          Math.max(0, previousAnchor.offset),
          anchorOffsetLimit(previousAnchor, anchorIndex),
        );
        nextTop = terminalScreenOffset() + virtualIndex.offset(anchorIndex) + anchorOffset;
      }
    }

    if (!lastPreserveLayout || resizeLayoutPending) virtualContentColumns = 0;
    else {
      virtualContentColumns = terminalScreenColumns(
        renderedRows,
        resizeLayoutActive,
        lastLeasedColumns || renderedResizeColumns,
      );
    }
    renderVirtualWindow(nextTop, true);
    return nextTop;
  }

  // Each row renders through a keyed each so a new frame patches changed rows
  // in place instead of re-creating the whole window. That keeps an in-flight
  // text selection alive and stops the full-viewport repaint flicker on every
  // delta frame.
  function mountedVirtualRows(start: number, end: number): { index: number; html: string }[] {
    const rows: { index: number; html: string }[] = [];
    for (let index = start; index < end; index += 1) {
      const attributes = renderedRows[index].wideGrid && wideGridBlocks[index] >= 0
        ? `<span data-terminal-row="${index}" data-terminal-wide-block="${wideGridBlocks[index]}" `
        : `<span data-terminal-row="${index}" `;
      rows.push({ index, html: renderedRows[index].html.replace('<span ', attributes) });
    }
    return rows;
  }

  // Contiguous wide box-drawn rows form one logical table. Their borders were
  // collapsed to separators, so a separator between two wide rows continues
  // the block instead of splitting the table into per-row scroll islands.
  const wideGridBlocks = $derived.by(() => {
    const blocks = new Array<number>(renderedRows.length).fill(-1);
    let block = -1;
    let open = false;
    for (let index = 0; index < renderedRows.length; index += 1) {
      if (renderedRows[index].wideGrid) {
        if (!open) {
          block += 1;
          open = true;
        }
        blocks[index] = block;
      } else if (!renderedRows[index].separator) {
        open = false;
      }
    }
    return blocks;
  });

  function syncWideGridScroll(event: Event) {
    const row = event.target;
    if (!(row instanceof HTMLElement) || row.dataset.terminalWideBlock === undefined || !terminalElement) return;
    const block = Number(row.dataset.terminalWideBlock);
    if (wideGridOffsetsPane !== agent.pane_id) {
      wideGridOffsets.clear();
      wideGridOffsetsPane = agent.pane_id;
    }
    if (wideGridOffsets.get(block) === row.scrollLeft) return;
    wideGridOffsets.set(block, row.scrollLeft);
    for (const sibling of terminalElement.querySelectorAll<HTMLElement>(`[data-terminal-wide-block="${block}"]`)) {
      if (sibling !== row && sibling.scrollLeft !== row.scrollLeft) sibling.scrollLeft = row.scrollLeft;
    }
  }

  function restoreWideGridScroll() {
    if (!terminalElement || wideGridOffsetsPane !== agent.pane_id) return;
    for (const row of terminalElement.querySelectorAll<HTMLElement>('[data-terminal-wide-block]')) {
      const offset = wideGridOffsets.get(Number(row.dataset.terminalWideBlock));
      if (offset && row.scrollLeft !== offset) row.scrollLeft = offset;
    }
  }

  function renderVirtualWindow(scrollTop: number, force = false) {
    const viewportHeight = terminalElement?.clientHeight || window.innerHeight;
    const viewportTop = Number.isFinite(scrollTop)
      ? scrollTop
      : Math.max(0, virtualIndex.total - viewportHeight);
    const range = virtualIndex.range(viewportTop, viewportHeight, viewportHeight * 1.5);
    const unchanged = range.start === virtualStart && range.end === virtualEnd;
    virtualStart = range.start;
    virtualEnd = range.end;
    virtualTopHeight = range.top;
    virtualBottomHeight = range.bottom;
    if (force || !unchanged) {
      virtualRows = mountedVirtualRows(range.start, range.end);
      queueVirtualRowObservation();
    }
  }

  function queueVirtualRowObservation() {
    void tick().then(observeVirtualRows);
  }

  function observeVirtualRows() {
    if (!terminalElement) return;
    restoreWideGridScroll();
    if (typeof ResizeObserver === 'undefined') return;
    virtualRowObserver ||= new ResizeObserver(measureVirtualRows);
    virtualRowObserver.disconnect();
    for (const row of terminalElement.querySelectorAll<HTMLElement>('[data-terminal-row]')) {
      virtualRowObserver.observe(row);
    }
  }

  function measureVirtualRows(entries: ResizeObserverEntry[]) {
    if (!terminalElement || !entries.length) return;
    if (virtualScrollResetPending) return;
    const previousTop = touchHeld ? docScrollTop() : terminalElement.scrollTop;
    const wasAtBottom = virtualStickToBottom;
    // The anchor must be the row the reader actually sees at the viewport top —
    // indexAt(previousTop) answered from the STALE index, which lands dozens of
    // rows too deep once heights drift, so deltas of rows below the viewport
    // were being added and the view walked down on every measure pass.
    const anchorRecord = currentVirtualAnchor(previousTop);
    const anchor = anchorRecord?.index
      ?? virtualIndex.indexAt(Math.max(0, previousTop - terminalScreenOffset()));
    let anchorDelta = 0;
    let changed = false;
    for (const entry of entries) {
      const element = entry.target as HTMLElement;
      const index = Number.parseInt(element.dataset.terminalRow || '', 10);
      if (!Number.isInteger(index) || index < 0 || index >= renderedRows.length) continue;
      const borderSize = Array.isArray(entry.borderBoxSize)
        ? entry.borderBoxSize[0]
        : entry.borderBoxSize;
      let height = borderSize?.blockSize || entry.contentRect.height;
      if (renderedRows[index].separator) {
        const style = getComputedStyle(element);
        height += (Number.parseFloat(style.marginTop) || 0)
          + (Number.parseFloat(style.marginBottom) || 0);
      }
      const delta = virtualIndex.update(index, height);
      if (!delta) continue;
      virtualHeightCache.set(renderedRows[index].html, height);
      measuredRowSizes.set(index, height);
      if (index < anchor) anchorDelta += delta;
      changed = true;
    }
    if (!changed) return;
    const nextTop = wasAtBottom ? virtualIndex.total : previousTop + anchorDelta;
    virtualScrollResetPending = true;
    renderVirtualWindow(touchHeld ? docScrollTop() : nextTop);
    void tick().then(() => {
      if (!terminalElement) {
        virtualScrollResetPending = false;
        return;
      }
      // Read the pin at apply time: an up-scroll can clear it between the
      // measure pass and this tick — honour the user's position, don't slam.
      const stillStuck = virtualStickToBottom;
      // A held touch owns the absolute position — a stick write never runs —
      // but the measured-height delta still applies over the live scrollTop so
      // spacer churn cannot slide the row out from under the finger.
      if (touchHeld) {
        // Measured heights above the anchor changed: fold the delta into the
        // transform — scrollTop stays untouched for the whole hold.
        applyHeldAnchor(terminalElement, anchorRecord, anchorDelta);
        rememberVirtualScrollGeometry(terminalElement);
        virtualScrollResetPending = false;
        return;
      }
      terminalElement.scrollTop = stillStuck ? terminalElement.scrollHeight : nextTop;
      if (!stillStuck
          && terminalElement.scrollHeight - terminalElement.scrollTop
              - terminalElement.clientHeight < 1) {
        suppressBottomPinOnce = true;
      }
      rememberVirtualScrollGeometry(terminalElement);
      virtualScrollResetPending = false;
    });
  }

  function scheduleVirtualWindow() {
    if (virtualWindowFrame) return;
    virtualWindowFrame = requestAnimationFrame(() => {
      virtualWindowFrame = 0;
      if (terminalElement) renderVirtualWindow(docScrollTop());
    });
  }

  function clearTerminalFindHighlights() {
    if (!terminalElement) return;
    const parents = new Set<Node>();
    for (const mark of terminalElement.querySelectorAll<HTMLElement>('mark[data-terminal-find]')) {
      if (mark.parentNode) parents.add(mark.parentNode);
      mark.replaceWith(document.createTextNode(mark.textContent || ''));
    }
    for (const parent of parents) parent.normalize();
  }

  function applyTerminalFindHighlights() {
    clearTerminalFindHighlights();
    if (!terminalElement || !findOpen || !findQuery.trim() || !terminalFind.matches.length) return;
    const byRow = new Map<number, Array<{ start: number; end: number; active: boolean }>>();
    terminalFind.matches.forEach((match, matchIndex) => {
      for (const fragment of terminalMatchFragments(renderedRows, terminalFindCorpus.offsets, match)) {
        if (fragment.row < virtualStart || fragment.row >= virtualEnd) continue;
        const fragments = byRow.get(fragment.row) || [];
        fragments.push({ start: fragment.start, end: fragment.end, active: matchIndex === activeFindIndex });
        byRow.set(fragment.row, fragments);
      }
    });
    for (const [row, fragments] of byRow) {
      const element = terminalElement.querySelector<HTMLElement>(`[data-terminal-row="${row}"]`);
      if (element) highlightTerminalRow(element, fragments);
    }
  }

  function highlightTerminalRow(
    element: HTMLElement,
    fragments: Array<{ start: number; end: number; active: boolean }>,
  ) {
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
    const nodes: Array<{ node: Text; start: number; end: number }> = [];
    let offset = 0;
    let node = walker.nextNode();
    while (node) {
      const text = node as Text;
      nodes.push({ node: text, start: offset, end: offset + text.data.length });
      offset += text.data.length;
      node = walker.nextNode();
    }
    for (const entry of nodes) {
      const intersections = fragments
        .map((fragment) => ({
          start: Math.max(fragment.start, entry.start) - entry.start,
          end: Math.min(fragment.end, entry.end) - entry.start,
          active: fragment.active,
        }))
        .filter((fragment) => fragment.end > fragment.start)
        .sort((left, right) => right.start - left.start);
      for (const fragment of intersections) {
        entry.node.splitText(fragment.end);
        const selected = entry.node.splitText(fragment.start);
        const mark = document.createElement('mark');
        mark.dataset.terminalFind = '';
        mark.className = `terminal-find-match${fragment.active ? ' active' : ''}`;
        selected.replaceWith(mark);
        mark.append(selected);
      }
    }
  }

  function openTerminalFind() {
    arrowsOpen = false;
    ctrlChordMenuOpen = false;
    findOpen = true;
    void tick().then(() => {
      findInputElement?.focus();
      findInputElement?.select();
    });
  }

  // The header owns the Find control, so the terminal exposes opening it rather
  // than lifting findOpen out: kept here, the bar still closes with the pane.
  export function openFind() {
    openTerminalFind();
  }

  function closeTerminalFind() {
    findOpen = false;
  }

  function findInputChanged() {
    activeFindIndex = -1;
    void tick().then(() => {
      if (terminalFind.matches.length) revealFindMatch(0);
    });
  }

  function findKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeTerminalFind();
      return;
    }
    if (event.key !== 'Enter') return;
    event.preventDefault();
    revealFindMatch(activeFindIndex + (event.shiftKey ? -1 : 1));
  }

  function revealFindMatch(index: number) {
    const count = terminalFind.matches.length;
    if (!terminalElement || !count) return;
    const normalized = ((index % count) + count) % count;
    activeFindIndex = normalized;
    const match = terminalFind.matches[normalized];
    const row = terminalRowForOffset(renderedRows, terminalFindCorpus.offsets, match.start);
    if (row < 0) return;
    const rowTop = terminalScreenOffset() + virtualIndex.offset(row);
    const nextTop = Math.max(0, rowTop - (terminalElement.clientHeight - virtualIndex.size(row)) / 2);
    virtualStickToBottom = false;
    virtualScrollResetPending = true;
    renderVirtualWindow(nextTop, true);
    void tick().then(() => {
      if (!terminalElement) return;
      terminalElement.scrollTop = nextTop;
      rememberVirtualScrollGeometry(terminalElement);
      virtualStickToBottom = terminalElement.scrollHeight
        - terminalElement.scrollTop
        - terminalElement.clientHeight < 48;
      jumpVisible = !virtualStickToBottom;
      virtualScrollResetPending = false;
      applyTerminalFindHighlights();
    });
  }

  function focusComposer(event: FocusEvent) {
    const target = event.target;
    if (!(target instanceof HTMLTextAreaElement)
      && !(target instanceof HTMLInputElement && target.classList.contains('question-other-input'))) return;
    composerFocused = true;
  }

  function blurComposer() {
    setTimeout(() => {
      const active = document.activeElement;
      if (active instanceof HTMLTextAreaElement
        || (active instanceof HTMLInputElement && active.classList.contains('question-other-input'))) return;
      composerFocused = false;
    });
  }


  function toggleDictation() {
    if ($dictationState === 'listening') {
      stopDictation();
      return;
    }
    dictationBase = composer;
    const started = startDictation(
      $speechLanguage,
      (finalText, interimText) => {
        const separator = dictationBase && (finalText || interimText) && !dictationBase.endsWith(' ') ? ' ' : '';
        composer = dictationBase + separator + finalText + interimText;
      },
      (message) => relayStore.showToast(message, true),
    );
    if (!started) dictationBase = '';
  }

  async function sendPrompt() {
    const submittedDraft = composer;
    const terminalText = terminalTextMode;
    const text = terminalText === 'filter' ? submittedDraft : submittedDraft.replace(/[\r\n]+$/g, '');
    if (!text || composerLocked || sendingPrompt) return;
    if ($dictationState === 'listening') stopDictation();
    if (terminalText === 'filter') {
      if (keySending || keyQueue.length) return;
      if (!/^[a-zA-Z0-9 ._/:+()[\]-]{1,32}$/.test(text) || text.endsWith(' ')) {
        relayStore.showToast('Use 1–32 letters, digits, spaces or .-_/+:()[]; no trailing space.', true);
        return;
      }
    }
    const target = agent;
    const targetIdentity = targetRefForAgent(target);
    sendingFilter = terminalText === 'filter';
    sendingPrompt = true;
    composer = '';
    clearPromptDraft(target);
    try {
      if (terminalText === 'filter') {
        await relayStore.sendToAgent(target, {
          type: 'send_filter_text', text, activity_label: 'Sent filter text',
        }, 15_000);
      } else if (terminalText === 'submit') {
        await relayStore.sendToAgent(target, {
          type: 'send_input', text, keys: ['Enter'], activity_label: 'Submitted terminal text',
        });
      } else {
        await relayStore.sendToAgent(target, { type: 'submit_prompt', text });
      }
      if (terminalText === 'filter') relayStore.showToast('Filter text sent. Select separately using terminal controls.');
    } catch (error) {
      const dispatchedUnknown = typeof error === 'object'
        && error !== null
        && 'data' in error
        && typeof error.data === 'object'
        && error.data !== null
        && 'dispatched_unknown' in error.data
        && error.data.dispatched_unknown === true;
      const notStarted = typeof error === 'object' && error !== null && 'data' in error
        && typeof error.data === 'object' && error.data !== null
        && 'not_started' in error.data && error.data.not_started === true;
      if (targetIdentity && targetRefMatchesAgent(targetIdentity, agent)
        && !composer && !dispatchedUnknown && (terminalText !== 'filter' || notStarted)) composer = submittedDraft;
      const detail = error instanceof Error
        ? error.message
        : terminalText === 'filter' ? 'Filter text could not be sent.' : terminalText ? 'Terminal text could not be submitted.' : 'Prompt could not be sent.';
      relayStore.showToast(dispatchedUnknown ? `${detail} Check the terminal before sending again.` : detail, true);
    } finally {
      sendingPrompt = false;
      sendingFilter = false;
      setTimeout(() => relayStore.readPane(target), 500);
    }
  }

  // ⌃⏎: mirror omp's ctrl+enter = app.message.followUp. The composer text must
  // reach the omp editor first (typed via send_input, no Enter — same first
  // half of what Send does), then the raw xterm modifyOtherKeys ctrl+enter
  // sequence queues it. herdr has no ctrl+enter chord, hence kind:'text'.
  async function sendCtrlEnter() {
    if (readOnly || keySending) return;
    const text = composer.replace(/[\r\n]+$/g, '');
    const target = agent;
    if (text) {
      composer = '';
      clearPromptDraft(target);
      try {
        await relayStore.sendToAgent(target, { type: 'send_input', text, activity_label: 'Queued follow-up text' });
      } catch (error) {
        composer = text;
        relayStore.showToast(error instanceof Error ? error.message : 'Could not stage the follow-up text.', true);
        return;
      }
    }
    // Small settle gap so omp's editor has the text before the chord lands.
    await new Promise((resolve) => setTimeout(resolve, 120));
    pushDirectCommand({ kind: 'text', text: '\x1b[27;5;13~', label: 'Ctrl+Enter' });
  }

  // Wide view toggles the lease column target (relay cap) and re-leases. The
  // pane reflows on the desktop too — documented trade-off, toggling back
  // restores the phone-measured width.
  function toggleWideView() {
    wideView = !wideView;
    requestPaneSizeLease(true);
    relayStore.showToast(wideView
      ? 'Wide view: pane leased at 240 columns. Swipe sideways to pan.'
      : 'Wide view off.');
  }

  async function submitSecret() {
    const secret = secretValue;
    if (!secret || !secretMode || sendingSecret) return;
    sendingSecret = true;
    try {
      await relayStore.sendSecret(agent, secret);
      secretValue = '';
      relayStore.showToast('Password sent to the terminal.');
    } catch (error) {
      // The value stays in the field for a retry; it is never stored.
      const message = error instanceof Error && error.message
        ? error.message
        : 'The password could not be sent.';
      relayStore.showToast(message, true);
    } finally {
      sendingSecret = false;
      setTimeout(() => relayStore.readPane(agent), 500);
    }
  }

  function secretKeydown(event: KeyboardEvent) {
    if (event.isComposing || event.key !== 'Enter') return;
    event.preventDefault();
    void submitSecret();
  }

  function composerInput() {
    if (dismissedSlashQuery !== composer) dismissedSlashQuery = null;
    activeSlashIndex = 0;
  }

  function clearComposer() {
    composer = '';
    dismissedSlashQuery = null;
    activeSlashIndex = 0;
  }

  function resizeComposer() {
    if (!composerElement) return;
    composerElement.style.height = 'auto';
    const maxHeight = Number.parseFloat(getComputedStyle(composerElement).maxHeight);
    const contentHeight = composerElement.scrollHeight;
    const capped = Number.isFinite(maxHeight) && contentHeight > maxHeight;
    composerElement.style.height = `${capped ? maxHeight : contentHeight}px`;
    composerElement.style.overflowY = capped ? 'auto' : 'hidden';
  }

  async function selectSlashCommand(command: SlashCommand) {
    composer = `${command.command}${command.argument_hint ? ' ' : ''}`;
    dismissedSlashQuery = composer;
    activeSlashIndex = 0;
    await tick();
    composerElement.focus();
    composerElement.setSelectionRange(composer.length, composer.length);
  }

  function keydown(event: KeyboardEvent) {
    if (event.isComposing) return;
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      void sendPrompt();
      return;
    }
    if (!slashMenuOpen) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      dismissedSlashQuery = composer;
      return;
    }
    if (event.key === 'ArrowDown' && filteredSlashCommands.length) {
      event.preventDefault();
      activeSlashIndex = effectiveSlashIndex >= filteredSlashCommands.length - 1 ? 0 : effectiveSlashIndex + 1;
      return;
    }
    if (event.key === 'ArrowUp' && filteredSlashCommands.length) {
      event.preventDefault();
      activeSlashIndex = effectiveSlashIndex <= 0 ? filteredSlashCommands.length - 1 : effectiveSlashIndex - 1;
      return;
    }
    if ((event.key === 'Enter' || event.key === 'Tab') && effectiveSlashIndex >= 0) {
      event.preventDefault();
      void selectSlashCommand(filteredSlashCommands[effectiveSlashIndex]);
    }
  }

  function sendKeys(keys: string[], activityLabel = '', flashId = ''): Promise<boolean> {
    if (readOnly || sendingFilter) return Promise.resolve(false);
    // Acknowledge the tap immediately — the flash is the feedback, not the
    // (layout-shifting) status bar.
    if (flashId) flashKey(flashId);
    return new Promise((resolve) => {
      keyQueue.push({ keys, label: activityLabel || keys.join(', '), flashId: flashId || undefined, resolve });
      void drainKeyQueue();
    });
  }

  async function drainKeyQueue() {
    if (keySending) return;
    keyRequestSending = true;
    while (keyQueue.length) {
      const command = keyQueue.shift()!;
      try {
        if (command.kind === 'input') {
          await relayStore.sendToAgent(agent, {
            type: 'send_input',
            text: command.text || '',
            keys: command.keys || [],
            activity_label: command.label || 'Typed terminal input',
          });
        } else if (command.kind === 'text') {
          await relayStore.sendToAgent(agent, {
            type: 'send_text',
            text: command.text || '',
            activity_label: command.label || 'Sent terminal bytes',
          });
        } else {
          await relayStore.sendToAgent(agent, {
            type: 'send_keys',
            keys: command.keys || [],
            activity_label: command.label,
          });
        }
        command.resolve(true);
        if (keyReadTimer) clearTimeout(keyReadTimer);
        keyReadTimer = setTimeout(() => {
          if (componentMounted) relayStore.readPane(agent);
        }, 300);
      } catch (error) {
        command.resolve(false);
        const message = error instanceof Error ? error.message : 'Terminal keys could not be sent.';
        if (command.flashId) flashKey(command.flashId, true);
        relayStore.showToast(message, true);
        for (const queued of keyQueue.splice(0)) queued.resolve(false);
      }
    }
    keyRequestSending = false;
  }

  function flashKey(id: string, error = false) {
    if (!componentMounted) return;
    if (keyFlashTimer) clearTimeout(keyFlashTimer);
    keyFlashId = id;
    keyFlashError = error;
    keyFlashTimer = setTimeout(() => {
      keyFlashId = '';
      keyFlashError = false;
    }, error ? 900 : 420);
  }

  function flashClass(id: string): string {
    return keyFlashId === id ? `key-flash${keyFlashError ? ' key-flash-error' : ''}` : '';
  }

  let fetchingSpeechText = $state(false);

  interface AgentResponseSource {
    text: string;
    /** The agent's own text (relay copy or its transcript), not a terminal parse. */
    exact: boolean;
    failure: string;
  }

  /**
   * The agent's latest complete response, from the most exact source this
   * device may use. A controller runs the relay's copy transaction, which types
   * into the agent's terminal; a reader may not, so it reads the agent's own
   * transcript instead. The terminal parse is the last resort for both.
   * `haltOnCopyFailure` returns the relay's failure without a fallback.
   */
  async function latestAgentResponse(haltOnCopyFailure = false): Promise<AgentResponseSource> {
    let failure = '';
    if (!readOnly && agentResponseCopySupported) {
      try {
        const result = await relayStore.sendToAgent(agent, { type: 'copy_agent_response' }, 15_000);
        const text = String(result.data?.text || '');
        if (text.trim()) return { text, exact: true, failure: '' };
      } catch (error) {
        failure = error instanceof Error && error.message ? error.message : 'Could not copy the agent response.';
        if (haltOnCopyFailure) return { text: '', exact: false, failure };
      }
    }
    if (agent.conversation_history_available) {
      try {
        const page = await relayStore.getConversationHistory(agent, { limit: 8 });
        const latest = page.entries.findLast((entry) => entry.role === 'assistant' && entry.text.trim());
        if (latest) return { text: latest.text, exact: true, failure: '' };
      } catch (error) {
        failure ||= error instanceof Error && error.message ? error.message : 'Could not read the conversation.';
      }
    }
    return { text: terminalCopyText, exact: false, failure };
  }

  async function speakTerminalResponse() {
    if ($speechState === 'speaking') {
      stopSpeech();
      return;
    }
    if (fetchingSpeechText) return;
    const toast = (message: string) => relayStore.showToast(message, true);
    // Checked before anything plays: unlocking audio for a language the relay
    // cannot speak leaves the phone with a silent stream and no explanation.
    // The device engine speaks whatever the phone has, so it skips this check.
    if ($speechEngine === 'relay' && !relaySpeechLanguages.includes($speechLanguage)) {
      toast(`This relay has no ${speechLanguageLabel($speechLanguage)} voice; install a Piper voice for it on that computer.`);
      return;
    }
    // Armed before the relay round trip: the tap's activation window does not
    // survive the await, and audio started after it is autoplay-blocked. The
    // device engine needs no media element, so arming is skipped for it.
    if ($speechEngine === 'relay') armSpeechKeepalive(toast);
    fetchingSpeechText = true;
    try {
      const { text, failure } = await latestAgentResponse();
      const spoke = text.trim() && speak(
        text,
        (chunk, language) => relayStore.speakToAgent(agent, chunk, language),
        toast,
      );
      if (!spoke) {
        releaseSpeechKeepalive();
        if (!text.trim()) toast(failure || 'No completed agent response is available to read aloud.');
      }
    } finally {
      fetchingSpeechText = false;
    }
  }

  async function copyTerminalOutput() {
    copiedAgentResponseText = '';
    let text = '';
    let copiedAgentResponse = false;
    copyingAgentResponse = true;
    try {
      const response = await latestAgentResponse(true);
      if (response.failure && !response.text.trim()) {
        relayStore.showToast(response.failure, true);
        return;
      }
      if (response.exact) {
        text = response.text;
        copiedAgentResponse = true;
        copiedAgentResponseText = response.text;
      }
    } finally {
      copyingAgentResponse = false;
    }
    if (!text) text = terminalCopyText || terminalPlainText;
    if (!text.trim()) {
      relayStore.showToast('No terminal output is available to copy.', true);
      return;
    }
    const hasCompletedResponse = copiedAgentResponse || Boolean(terminalCopyText.trim());
    const target = hasCompletedResponse ? responseElement : transcriptElement;
    const copiedMessage = copiedAgentResponse
      ? 'Agent response copied.'
      : hasCompletedResponse
        ? 'Final response copied.'
        : 'Copied the visible terminal output.';
    const selectedMessage = copiedAgentResponse
      ? 'Output selected. Use your browser Copy command.'
      : hasCompletedResponse
        ? 'Final response selected. Use your browser Copy command.'
        : 'Output selected. Use your browser Copy command.';
    if (!navigator.clipboard?.writeText) {
      target.value = text;
      target.focus({ preventScroll: true });
      target.select();
      relayStore.showToast(selectedMessage);
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      relayStore.showToast(copiedMessage);
    } catch {
      target.value = text;
      target.focus({ preventScroll: true });
      target.select();
      relayStore.showToast(selectedMessage);
    }
  }

  async function copyDisplayedAgentResponse() {
    const text = copiedAgentResponseText;
    if (!text.trim()) return;
    if (!navigator.clipboard?.writeText) {
      agentResponsePreviewElement.focus({ preventScroll: true });
      agentResponsePreviewElement.select();
      relayStore.showToast('Output selected. Use your browser Copy command.');
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      relayStore.showToast('Agent response copied.');
    } catch {
      agentResponsePreviewElement.focus({ preventScroll: true });
      agentResponsePreviewElement.select();
      relayStore.showToast('Output selected. Use your browser Copy command.');
    }
  }

  function dismissCopiedAgentResponse() {
    copiedAgentResponseText = '';
  }
  function toggleModifier(which: 'ctrl' | 'alt' | 'shift') {
    if (readOnly) return;
    arrowsOpen = false;
    fkeysOpen = false;
    ctrlChordMenuOpen = false;
    if (which === 'ctrl') ctrlArmed = !ctrlArmed;
    else if (which === 'alt') altArmed = !altArmed;
    else shiftArmed = !shiftArmed;
    if (ctrlArmed || altArmed || shiftArmed) {
      modifierInputElement.value = '';
      modifierInputElement.focus();
    } else {
      modifierInputElement.blur();
    }
  }

  function toggleCtrl() {
    toggleModifier('ctrl');
  }

  function toggleAlt() {
    toggleModifier('alt');
  }

  function toggleShift() {
    toggleModifier('shift');
  }

  function modifierChord(key: string): { chord: string; label: string } | null {
    const parts: string[] = [];
    const labels: string[] = [];
    if (ctrlArmed) { parts.push('ctrl'); labels.push('Ctrl'); }
    if (altArmed) { parts.push('alt'); labels.push('Alt'); }
    if (shiftArmed) { parts.push('shift'); labels.push('Shift'); }
    if (!parts.length) return null;
    parts.push(key.toLocaleLowerCase());
    labels.push(key.length === 1 ? key.toLocaleUpperCase() : key[0].toLocaleUpperCase() + key.slice(1).toLocaleLowerCase());
    return { chord: parts.join('+'), label: labels.join('+') };
  }

  function disarmModifiers() {
    ctrlArmed = false;
    altArmed = false;
    shiftArmed = false;
    modifierInputElement.blur();
  }

  function modifierInput(event: Event) {
    const target = event.currentTarget as HTMLInputElement;
    const character = Array.from(target.value)[0] || '';
    target.value = '';
    if (!character || /\s/u.test(character)) return;
    sendTerminalKey(character);
  }

  function sendTerminalKey(key: string, plainLabel = key, flashId = '') {
    const result = modifierChord(key);
    void sendKeys([result?.chord || key], result?.label || plainLabel, flashId);
  }

  function sendTab() {
    sendTerminalKey('Tab', 'Tab', 'tab');
  }

  // Tap ^ opens a chord menu (same pattern as the arrow/F-key pads): the
  // pane's own agent leads with its most useful chord (OMP's Ctrl+P role
  // switcher, Claude's Esc interrupt, …), universal control chords follow.
  // 'Arm Ctrl' keeps the old type-any-key chord flow reachable.
  const chordSet = $derived(agentChords(agent.agent));
  let ctrlChordMenuOpen = $state(false);

  function toggleCtrlMenu() {
    ctrlChordMenuOpen = !ctrlChordMenuOpen;
    fkeysOpen = false;
    arrowsOpen = false;
  }

  function sendCtrlChord(keys: string[], label: string) {
    ctrlChordMenuOpen = false;
    void sendKeys(keys, label, 'ctrl');
  }

  function armCtrlFromMenu() {
    ctrlChordMenuOpen = false;
    toggleModifier('ctrl');
    modifierInputElement.focus();
  }

  function sendFunctionKey(number: number) {
    fkeysOpen = false;
    // Herdr parses function keys as f1..f24; the label keeps the pad readable.
    sendTerminalKey(`f${number}`, `F${number}`, 'fkeys');
  }

  function modifierKeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape') return;
    event.preventDefault();
    disarmModifiers();
  }

  function modifierBlur() {
    setTimeout(() => {
      if (document.activeElement !== modifierInputElement) {
        ctrlArmed = false;
        altArmed = false;
        shiftArmed = false;
      }
    });
  }

  // ---- Direct typing -------------------------------------------------------
  // The hidden capture field mirrors Orca mobile's live input: whatever the
  // on-screen keyboard produces is forwarded to the pane immediately. Text
  // rides send_input (the typed, paste-aware socket call); named keys ride
  // send_input's semantic key set; keys Herdr has no name for (Home, End,
  // Delete, PageUp, PageDown, Insert) go as raw VT sequences via send_text.

  const DIRECT_INPUT_KEYS: Record<string, string> = {
    Enter: 'Enter',
    Escape: 'Esc',
    Tab: 'Tab',
    Backspace: 'Backspace',
    ArrowUp: 'Up',
    ArrowDown: 'Down',
    ArrowLeft: 'Left',
    ArrowRight: 'Right',
  };

  const DIRECT_VT_KEYS: Record<string, string> = {
    Home: '\x1b[H',
    End: '\x1b[F',
    Delete: '\x1b[3~',
    PageUp: '\x1b[5~',
    PageDown: '\x1b[6~',
    Insert: '\x1b[2~',
  };

  function pushDirectCommand(command: Omit<QueuedKeyCommand, 'resolve'>) {
    if (readOnly) return;
    // Coalesce consecutive typed text so a burst of characters travels as one
    // send_input instead of one request per keystroke.
    const tail = keyQueue[keyQueue.length - 1];
    if (command.kind === 'input' && !command.keys?.length && command.text
      && tail?.kind === 'input' && !tail.keys?.length && tail.text) {
      tail.text += command.text;
    } else {
      keyQueue.push({ ...command, resolve: () => {} });
    }
    void drainKeyQueue();
  }

  function directSendText(text: string) {
    if (!text) return;
    if (text.length === 1) {
      const chord = modifierChord(text);
      if (chord) {
        pushDirectCommand({ kind: 'keys', keys: [chord.chord], label: chord.label });
        return;
      }
    }
    pushDirectCommand({ kind: 'input', text, label: '' });
  }

  function directSendKey(key: string, label = key) {
    const chord = modifierChord(key);
    if (chord) {
      pushDirectCommand({ kind: 'keys', keys: [chord.chord], label: chord.label });
      return;
    }
    const semantic = DIRECT_INPUT_KEYS[key];
    if (semantic) {
      pushDirectCommand({ kind: 'input', keys: [semantic], label });
      return;
    }
    if (/^f(?:[1-9]|1[0-9]|2[0-4])$/i.test(key)) {
      pushDirectCommand({ kind: 'input', keys: [key.toUpperCase()], label: key.toUpperCase() });
      return;
    }
    const sequence = DIRECT_VT_KEYS[key];
    if (sequence) {
      pushDirectCommand({ kind: 'text', text: sequence, label });
      return;
    }
    // Last resort: hand the name to send_keys and let Herdr decide.
    pushDirectCommand({ kind: 'keys', keys: [key], label });
  }

  function focusDirectCapture() {
    if (!directInput || readOnly || !directInputElement) return;
    if (document.activeElement === directInputElement) return;
    directInputElement.focus();
  }

  function terminalSurfaceClick(event: MouseEvent) {
    if (!directInput) return;
    const target = event.target;
    if (target instanceof Element && target.closest('a,button,input,textarea')) return;
    focusDirectCapture();
  }

  function toggleDirectInput() {
    if (readOnly) return;
    directInput = !directInput;
    if (directInput) {
      void tick().then(() => focusDirectCapture());
    } else {
      directInputElement?.blur();
    }
  }

  function directKeydown(event: KeyboardEvent) {
    if (event.isComposing) return;
    const key = event.key;
    if (key === 'Backspace') {
      event.preventDefault();
      directBackspaceAt = Date.now();
      directSendKey('Backspace');
      return;
    }
    if (key === 'Enter' || key === 'Escape' || key === 'Tab'
      || key === 'ArrowUp' || key === 'ArrowDown' || key === 'ArrowLeft' || key === 'ArrowRight'
      || key === 'Home' || key === 'End' || key === 'Delete'
      || key === 'PageUp' || key === 'PageDown' || key === 'Insert'
      || /^F\d{1,2}$/.test(key)) {
      event.preventDefault();
      directSendKey(key);
      return;
    }
    // Hardware keyboards report real modifier state; fold it into a chord so
    // Ctrl+C and friends reach the pane as keys, not control characters.
    if ((event.ctrlKey || event.metaKey || event.altKey) && key.length === 1) {
      event.preventDefault();
      const parts: string[] = [];
      if (event.ctrlKey || event.metaKey) parts.push('ctrl');
      if (event.altKey) parts.push('alt');
      if (event.shiftKey && /[a-z]/i.test(key)) parts.push('shift');
      parts.push(key.toLocaleLowerCase());
      pushDirectCommand({ kind: 'keys', keys: [parts.join('+')], label: parts.join('+') });
    }
  }

  function directInputEvent(event: Event) {
    const target = event.currentTarget as HTMLTextAreaElement;
    const input = event as InputEvent;
    if (directComposing || input.isComposing) return;
    const type = input.inputType || '';
    if (type.startsWith('delete')) {
      // Some IMEs deliver Backspace only as an input event; the keydown path
      // already sent one within the same tick, so dedupe by timestamp.
      if (Date.now() - directBackspaceAt > 50) directSendKey('Backspace');
    } else if (type === 'insertParagraph' || type === 'insertLineBreak') {
      directSendKey('Enter');
    } else {
      const data = input.data ?? target.value;
      if (data === '\n' || data === '\r\n') {
        directSendKey('Enter');
      } else if (data) {
        directSendText(data);
      }
    }
    target.value = '';
  }

  function directCompositionStart() {
    directComposing = true;
  }

  function directCompositionEnd(event: CompositionEvent) {
    directComposing = false;
    const target = event.currentTarget as HTMLTextAreaElement;
    const data = event.data || target.value;
    target.value = '';
    if (data) directSendText(data);
  }


  function jumpToBottom() {
    // Under a held touch the pin is intent only: the bottom-window render
    // would leave the viewport over spacer for the whole gesture since the
    // scrollTop write below stays suppressed. Release re-derives the pin.
    if (touchHeld) { virtualStickToBottom = true; return; }
    virtualStickToBottom = true;
    virtualScrollResetPending = true;
    renderVirtualWindow(Number.POSITIVE_INFINITY, true);
    void tick().then(() => {
      if (!terminalElement) {
        virtualScrollResetPending = false;
        return;
      }
      if (touchHeld) { virtualScrollResetPending = false; return; }
      terminalElement.scrollTop = terminalElement.scrollHeight;
      virtualScrollResetPending = false;
      jumpVisible = false;
    });
  }

  function handleScroll() {
    // A scroll event queued during teardown can fire after Svelte has already
    // cleared the bind:this reference; there is nothing left to measure.
    if (!terminalElement) return;
    // Per-event deltas on a touch device are sub-pixel, so intent is the
    // accumulated upward travel, not the size of this one event.
    const scrollDelta = terminalElement.scrollTop - virtualScrollTop;
    if (scrollDelta < 0) scrollUpAccum += -scrollDelta;
    else if (scrollDelta > 0.5) scrollUpAccum = 0;
    const movedUp = scrollUpAccum > 2;
    // Any reposition during a resize wait invalidates the pending anchor.
    if (pendingResizeAnchor !== null && Math.abs(scrollDelta) >= 0.5) {
      pendingAnchorStale = true;
    }
    if (virtualScrollResetPending) {
      // An up-scroll while a reset apply is in flight is still user intent:
      // dropping it here left the pin alive and the pending apply slammed the
      // view back to the bottom.
      if (movedUp) {
        virtualStickToBottom = false;
        // A pending resize snapshot captured while pinned must die with the
        // pin — otherwise it re-arms on the apply that ends the wait and
        // slams the reader back to the bottom anyway.
        pendingResizeStick = null;
        pendingResizeAnchor = null;
        pendingLayoutStick = null;
      }
      rememberVirtualScrollGeometry(terminalElement);
      return;
    }
    if (touchHeld) {
      // The finger owns the viewport: scrollTop no longer equals the visual
      // position (heldOffset carries part of it), so the atBottom/pin math
      // below would misjudge. Upward intent still drops the pin immediately;
      // release re-derives everything from real geometry.
      if (movedUp) {
        virtualStickToBottom = false;
        pendingResizeStick = null;
        pendingResizeAnchor = null;
        pendingLayoutStick = null;
      }
      jumpVisible = !virtualStickToBottom;
      rememberVirtualScrollGeometry(terminalElement);
      scheduleVirtualWindow();
      return;
    }
    const scrollTop = terminalElement.scrollTop;
    const scrollHeight = terminalElement.scrollHeight;
    const clientHeight = terminalElement.clientHeight;
    const bottomDistance = scrollHeight - scrollTop - clientHeight;
    const atBottom = bottomDistance < 48;
    // Only a viewport/controls height change may re-pin: content growth also
    // changes scrollHeight, and a user scrolling up during a stream must win.
    const layoutChanged = Math.abs(clientHeight - virtualClientHeight) >= 1;
    // A scroll that lands exactly at the bottom is never the user moving
    // toward history: when corrected row heights shrink the content while
    // pinned, the browser clamps scrollTop to the new maximum and fires a
    // scroll event whose position is lower than the remembered one. Reading
    // that clamp as intent dropped stick-to-bottom, so Safari — whose real
    // row heights disagree with the estimates more than Chromium's — opened
    // a growing gap above the transcript's end and fought every scroll with
    // anchor-preserving corrections (issue #11's missing bottom + flicker).
    // A shrinking frame (a viewport-only refresh, a transient empty frame)
    // makes the browser clamp scrollTop down to the new maximum and fire a
    // scroll event that lands exactly at the shrunken bottom. That is the
    // clamp moving the viewport, not the reader — it must not re-arm the pin,
    // or the restored full content slams the view back to the end.
    const shrinkClamp = scrollHeight < virtualScrollHeight - 1;
    const movedTowardHistory = !layoutChanged
      && movedUp
      && bottomDistance > 1;
    rememberVirtualScrollGeometry(terminalElement);
    if (shrinkClamp && !movedTowardHistory) {
      jumpVisible = !virtualStickToBottom;
      scheduleVirtualWindow();
      return;
    }
    if (movedTowardHistory) {
      virtualStickToBottom = false;
      pendingResizeStick = null;
      pendingResizeAnchor = null;
      pendingLayoutStick = null;
      jumpVisible = true;
    } else if (atBottom) {
      if (suppressBottomPinOnce) {
        // A programmatic write just clamped at the bottom; consuming the event
        // without re-arming the pin keeps the reader where a correction put
        // them instead of slamming to the end on the next frame.
        suppressBottomPinOnce = false;
        jumpVisible = true;
        scheduleVirtualWindow();
        return;
      }
      if (bottomDistance > 0.5 && scrollDelta > 0) {
        if (pendingResizeAnchor !== null || pendingResizeStick !== null) {
          pendingResizeStick = true;
          pendingResizeAnchor = null;
        }
        jumpToBottom();
        return;
      }
      virtualStickToBottom = true;
      if (pendingResizeAnchor !== null) pendingResizeStick = true;
      jumpVisible = false;
    } else if (!virtualStickToBottom) {
      jumpVisible = true;
    } else {
      jumpToBottom();
      return;
    }
    scheduleVirtualWindow();
  }

  function paneSizeLeaseSupported(target: Agent): boolean {
    const connection = $connections.get(target.relay_id);
    return componentMounted
      && !readOnly
      && !questionMode
      && connection?.status === 'connected'
      && connection.capabilities.includes('pane_size_lease');
  }

  function paneSizeRowLeaseSupported(target: Agent): boolean {
    return $terminalHeightLease && Boolean(
      $connections.get(target.relay_id)?.capabilities.includes('pane_size_lease_rows'),
    );
  }

  // A toggle flips the next lease immediately: on adds the measured rows, off
  // renews width-only, which lifts this client's row constraint on the relay.
  $effect(() => {
    const enabled = $terminalHeightLease;
    void enabled;
    void tick().then(() => requestPaneSizeLease(false));
  });

  function measuredPaneColumns(): number | null {
    if (!terminalElement || !cellMeasureElement) return null;
    const cellWidth = cellMeasureElement.getBoundingClientRect().width / CELL_MEASURE_TEXT.length;
    const style = getComputedStyle(terminalElement);
    const horizontalPadding = (Number.parseFloat(style.paddingLeft) || 0)
      + (Number.parseFloat(style.paddingRight) || 0);
    const usableWidth = terminalElement.clientWidth - horizontalPadding;
    if (!Number.isFinite(cellWidth) || cellWidth <= 0 || usableWidth <= 0) return null;
    // The probed advance also drives every CSS width cap, so the caps stay
    // correct the moment the font or interface size changes.
    if (cellWidth !== measuredCellWidth) measuredCellWidth = cellWidth;
    return Math.min(
      MAX_PANE_SIZE_COLUMNS,
      Math.max(MIN_PANE_SIZE_COLUMNS, Math.floor(usableWidth / cellWidth)),
    );
  }

  // 0 means "do not lease rows": the relay keeps the pane's own height.
  function measuredPaneRows(): number {
    if (!terminalElement) return 0;
    const style = getComputedStyle(terminalElement);
    const lineHeight = Number.parseFloat(style.lineHeight);
    const verticalPadding = (Number.parseFloat(style.paddingTop) || 0)
      + (Number.parseFloat(style.paddingBottom) || 0);
    const usableHeight = terminalElement.clientHeight - verticalPadding;
    if (!Number.isFinite(lineHeight) || lineHeight <= 0 || usableHeight <= 0) return 0;
    return Math.min(
      MAX_PANE_SIZE_ROWS,
      Math.max(MIN_PANE_SIZE_ROWS, Math.floor(usableHeight / lineHeight)),
    );
  }

  function beginResizeSettling() {
    resizeFrameBaseline = frame;
    resizeWaitExpired = false;
    if (resizeWaitTimer !== null) clearTimeout(resizeWaitTimer);
    resizeWaitTimer = setTimeout(() => {
      resizeWaitTimer = null;
      resizeWaitExpired = true;
    }, PANE_RESIZE_WAIT_MAX_MS);
  }

  function clearResizeSettling() {
    resizeFrameBaseline = undefined;
    resizeWaitExpired = false;
    if (resizeWaitTimer === null) return;
    clearTimeout(resizeWaitTimer);
    resizeWaitTimer = null;
  }

  function discardPaneSizeLease() {
    leaseGeneration += 1;
    leaseTarget = null;
    lastLeasedColumns = 0;
    lastLeasedRows = 0;
    clearResizeSettling();
    queuedLease = null;
  }

  function releasePaneSizeLease(reportFailure: boolean) {
    const target = leaseTarget;
    discardPaneSizeLease();
    if (!target) return;
    void relayStore.releasePaneSize(target).catch((error) => {
      const connection = $connections.get(target.relay_id);
      if (reportFailure && componentMounted && connection?.status === 'connected') {
        paneSizeLeaseError = `Resize Session release failed: ${(error as Error).message}`;
      }
    });
  }

  function requestPaneSizeLease(force: boolean) {
    const target = agent;
    // A hidden page renews only within the grace window: after it, the
    // relay's lease TTL returns the desktop size, and the refocus handler
    // re-leases the moment the page is visible again.
    if (!paneLeaseAllowed()) return;
    if (!paneSizeLeaseSupported(target)) return;
    // Wide view leases the pane at the relay's cap so ASCII art and wide
    // tables render unwrapped; the view pans sideways to reveal them.
    let columns = wideView ? MAX_PANE_SIZE_COLUMNS : measuredPaneColumns();
    if (columns === null) {
      if (terminalElement && cellMeasureElement) {
        paneSizeLeaseError = 'Resize Session could not measure the terminal cell width.';
      }
      return;
    }
    let rows = paneSizeRowLeaseSupported(target) ? measuredPaneRows() : 0;
    // Font loading and sub-pixel rounding wobble the probe by a column or two
    // between mounts. Wobble must not fight the cache: snap to the cached size
    // when the measurement is within a couple of cells, so a steady-state tab
    // switch requests exactly the cached lease and never re-runs the settle
    // wait (the 'Resizing terminal…' placeholder).
    ({ columns, rows } = stabilizePaneSize(columns, rows, cachedPaneLease));
    // The on-screen keyboard shrinks the terminal while the user types, and
    // leasing that transient height would SIGWINCH the agent twice per
    // keyboard toggle. Every full-height redraw can strand a stale copy of a
    // bottom-anchored status bar in the scrollback, so while a text input
    // owns focus the lease keeps its resting height and may only grow; the
    // resize listeners re-measure the moment the keyboard closes.
    const active = document.activeElement;
    const typing = active instanceof HTMLTextAreaElement || active instanceof HTMLInputElement;
    if (typing && rows && lastLeasedRows && rows < lastLeasedRows) rows = lastLeasedRows;
    const sameTarget = leaseTarget?.pane_id === target.pane_id;
    if (!force && sameTarget && columns === lastLeasedColumns && rows === lastLeasedRows) return;
    if (queuedLease && queuedLease.columns === columns && queuedLease.rows === rows) {
      queuedLease = { columns, rows, force: queuedLease.force || force };
    } else queuedLease = { columns, rows, force };
    if (!leaseInFlight) void flushPaneSizeLease();
  }

  async function flushPaneSizeLease() {
    if (leaseInFlight) return;
    leaseInFlight = true;
    try {
      while (queuedLease) {
        const request = queuedLease;
        queuedLease = null;
        const target = agent;
        if (!paneSizeLeaseSupported(target)) continue;
        if (!request.force
          && leaseTarget?.pane_id === target.pane_id
          && request.columns === lastLeasedColumns
          && request.rows === lastLeasedRows) continue;
        if (leaseTarget && leaseTarget.pane_id !== target.pane_id) {
          releasePaneSizeLease(componentMounted);
        }
        const generation = leaseGeneration;
        leaseTarget = target;
        try {
          if (request.columns !== lastLeasedColumns
            || request.rows !== lastLeasedRows) beginResizeSettling();
          const applied = await relayStore.leasePaneSize(target, request.columns, request.rows);
          // Cache the applied size before the generation check: a quick tab
          // switch bumps the generation and skips the rest, but the relay DID
          // apply this size — skipping the write here is what kept the cache
          // empty and 'Resizing terminal…' on every switch.
          if (!wideView) storePaneSize(applied.columns, applied.rows);
          if (generation !== leaseGeneration
            || leaseTarget?.pane_id !== target.pane_id
            || !paneSizeLeaseSupported(target)) continue;
          const changed = applied.columns !== lastLeasedColumns
            || applied.rows !== lastLeasedRows;
          lastLeasedColumns = applied.columns;
          lastLeasedRows = applied.rows;
          paneSizeLeaseError = '';
          if (changed || !leaseReadDone) {
            // The pane repaints at the new size: read again so the live
            // screen is the resized one. History stays with the relay journal.
            relayStore.readPane(target, true);
          }
          leaseReadDone = true;
        } catch (error) {
          if (generation === leaseGeneration
            && leaseTarget?.pane_id === target.pane_id
            && paneSizeLeaseSupported(target)) {
            queuedLease = null;
            lastLeasedColumns = 0;
            lastLeasedRows = 0;
            clearResizeSettling();
            paneSizeLeaseError = `Resize Session failed: ${(error as Error).message}`;
          }
        }
      }
    } finally {
      leaseInFlight = false;
      if (queuedLease) void flushPaneSizeLease();
    }
  }


  function appendUploadedAttachments(attachments: AttachmentRef[]): void {
    const rejected = attachmentSnapshot?.items.filter((item) => item.state === 'rejected') || [];
    if (!attachments.length) {
      uploadStatus = attachmentCancelRequested
        ? 'Attachment upload canceled.'
        : rejected.length
          ? 'No selected attachments passed validation.'
          : 'No attachments were uploaded.';
      uploadError = !attachmentCancelRequested;
      return;
    }
    const prefix = composer && !composer.endsWith('\n') ? '\n' : '';
    composer += `${prefix}${attachments.map((attachment) => `Attachment: ${attachment.ref}`).join('\n')}\n`;
    uploadStatus = `Attached ${attachments.map((attachment) => attachment.name).join(', ')}${rejected.length ? `; ${rejected.length} rejected` : ''}`;
    uploadError = rejected.length > 0;
    if (!rejected.length) attachmentSnapshot = null;
  }

  function releaseAttachmentController(controller: AttachmentBatchController, force = false): void {
    if (!force && attachmentSnapshot?.items.some((item) => item.state === 'interrupted')) return;
    attachmentUnsubscribe?.();
    attachmentUnsubscribe = null;
    if (attachmentController === controller) attachmentController = null;
  }

  async function filesSelected(files: FileList | File[]) {
    const selected = [...files];
    if (inputLocked || !selected.length || uploadingAttachment) return;
    uploadingAttachment = true;
    uploadStatus = `Uploading ${selected.length} attachment${selected.length === 1 ? '' : 's'}…`;
    uploadError = false;
    attachmentCancelRequested = false;
    let controller: AttachmentBatchController | null = null;
    try {
      const previous = attachmentController;
      if (previous) {
        try {
          await previous.cancel();
        } finally {
          releaseAttachmentController(previous, true);
        }
      }
      controller = relayStore.attachmentController(agent);
      attachmentController = controller;
      attachmentUnsubscribe?.();
      attachmentUnsubscribe = controller.subscribe((snapshot) => {
        attachmentSnapshot = snapshot;
      });
      controller.select(selected);
      const attachments = await controller.upload();
      appendUploadedAttachments(attachments);
    } catch (error) {
      uploadStatus = attachmentCancelRequested
        ? 'Attachment upload canceled.'
        : error instanceof Error && error.message
          ? error.message
          : 'Attachments could not be uploaded.';
      uploadError = !attachmentCancelRequested;
    } finally {
      uploadingAttachment = false;
      if (controller) releaseAttachmentController(controller);
    }
  }
  async function restartAttachmentUpload(): Promise<void> {
    const controller = attachmentController;
    if (inputLocked || !controller || uploadingAttachment) return;
    uploadingAttachment = true;
    uploadStatus = 'Restarting interrupted files from the beginning…';
    uploadError = false;
    attachmentCancelRequested = false;
    try {
      appendUploadedAttachments(await controller.restart());
    } catch (error) {
      uploadStatus = error instanceof Error && error.message
        ? error.message
        : 'Attachments could not be restarted.';
      uploadError = true;
    } finally {
      uploadingAttachment = false;
      releaseAttachmentController(controller);
    }
  }


  async function cancelAttachmentUpload(): Promise<void> {
    const controller = attachmentController;
    if (!controller) return;
    attachmentCancelRequested = true;
    try {
      await controller.cancel();
      attachmentSnapshot = null;
    } catch {
      uploadStatus = 'The relay could not confirm attachment cancellation.';
      uploadError = true;
    } finally {
      releaseAttachmentController(controller, true);
    }
  }

  onDestroy(() => {
    attachmentUnsubscribe?.();
    void attachmentController?.cancel();
  });

  function paste(event: ClipboardEvent) {
    const files = [...(event.clipboardData?.items || [])]
      .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
      .map((item) => item.getAsFile())
      .filter((file): file is File => Boolean(file));
    if (!files.length) return;
    event.preventDefault();
    void filesSelected(files);
  }

  function menuKeyLabel(keys: string[]): string {
    return keys.map((key) => key === ' ' ? 'Space' : key).join('+');
  }

  function openNext() {
    if (nextBlocked) {
      replaceView({
        view: 'terminal',
        paneId: nextBlocked.pane_id,
        target: targetRefForAgent(nextBlocked) || undefined,
      });
    }
  }
</script>

{#snippet arrowIcon()}
  <svg class="key-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="M12 2v20M2 12h20"></path>
    <path d="m8 6 4-4 4 4M8 18l4 4 4-4M6 8l-4 4 4 4M18 8l4 4-4 4"></path>
  </svg>
{/snippet}

{#snippet arrowUpIcon()}
  <svg class="key-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="M12 19V5M5 12l7-7 7 7"></path>
  </svg>
{/snippet}

{#snippet arrowDownIcon()}
  <svg class="key-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="M12 5v14M5 12l7 7 7-7"></path>
  </svg>
{/snippet}

{#snippet arrowLeftIcon()}
  <svg class="key-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="M19 12H5M12 19l-7-7 7-7"></path>
  </svg>
{/snippet}

{#snippet arrowRightIcon()}
  <svg class="key-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="M5 12h14M12 5l7 7-7 7"></path>
  </svg>
{/snippet}

<!-- Drawn rather than typed: ⇥ and ⇧ resolve to a different fallback font on
     each platform, and their glyphs sit at different heights inside the em box,
     so a text label cannot be centred for Android and the desktop at once. -->
{#snippet tabIcon()}
  <svg class="key-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="M3 12h12M11 8l4 4-4 4M20 6v12"></path>
  </svg>
{/snippet}

{#snippet shiftIcon()}
  <svg class="key-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="M12 4.5 4 12.5h4v7h8v-7h4z"></path>
  </svg>
{/snippet}

{#snippet queueIcon()}
  <svg class="key-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="M17 3h4v4"></path>
    <path d="M21 3 12.5 11.5"></path>
    <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h7"></path>
    <path d="M8 16h8"></path>
    <path d="M8 12h5"></path>
  </svg>
{/snippet}
{#snippet wideIcon()}
  <svg class="button-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="m3 12 4-4m-4 4 4 4m-4-4h8"></path>
    <path d="m21 12-4-4m4 4-4 4m4-4h-8"></path>
  </svg>
{/snippet}

{#snippet copyIcon()}
  <svg class="button-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <rect x="9" y="9" width="11" height="12" rx="2"></rect>
    <path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"></path>
  </svg>
{/snippet}
{#snippet keyboardIcon()}
  <svg class="button-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <rect x="2" y="5" width="20" height="14" rx="2"></rect>
    <path d="M6 9h.01M10 9h.01M14 9h.01M18 9h.01M6 13h.01M18 13h.01M9 13h6M7 17h10"></path>
  </svg>
{/snippet}
{#snippet speakerIcon()}
  <svg class="button-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="M11 5 6 9H2v6h4l5 4z"></path>
    <path d="M15.54 8.46a5 5 0 0 1 0 7.07"></path>
    <path d="M19.07 4.93a10 10 0 0 1 0 14.14"></path>
  </svg>
{/snippet}
{#snippet stopIcon()}
  <svg class="button-symbol" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false">
    <rect x="6" y="6" width="12" height="12" rx="2"></rect>
  </svg>
{/snippet}
{#snippet findPreviousIcon()}
  <svg class="find-action-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="m6 15 6-6 6 6"></path>
  </svg>
{/snippet}

{#snippet findNextIcon()}
  <svg class="find-action-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="m6 9 6 6 6-6"></path>
  </svg>
{/snippet}

{#snippet findCloseIcon()}
  <svg class="find-action-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="m6 6 12 12M18 6 6 18"></path>
  </svg>
{/snippet}


{#snippet arrowPopup()}
  {#if arrowsOpen}
    <div class="arrow-popup">
      <span aria-hidden="true"></span>
      <button class={flashClass('up')} disabled={readOnly} aria-label="Up" onpointerdown={(event) => event.preventDefault()} onclick={() => sendTerminalKey('Up', 'Up', 'up')}>↑</button>
      <span aria-hidden="true"></span>
      <button class={flashClass('left')} disabled={readOnly} aria-label="Left" onpointerdown={(event) => event.preventDefault()} onclick={() => sendTerminalKey('Left', 'Left', 'left')}>←</button>
      <span aria-hidden="true"></span>
      <button class={flashClass('right')} disabled={readOnly} aria-label="Right" onpointerdown={(event) => event.preventDefault()} onclick={() => sendTerminalKey('Right', 'Right', 'right')}>→</button>
      <span aria-hidden="true"></span>
      <button class={flashClass('down')} disabled={readOnly} aria-label="Down" onpointerdown={(event) => event.preventDefault()} onclick={() => sendTerminalKey('Down', 'Down', 'down')}>↓</button>
      <span aria-hidden="true"></span>
    </div>
  {/if}
{/snippet}

{#snippet fkeyPopup()}
  {#if fkeysOpen}
    <div class="fkey-popup" role="group" aria-label="Function keys">
      {#each FUNCTION_KEYS as number (number)}
        <button
          disabled={readOnly}
          onpointerdown={(event) => event.preventDefault()}
          onclick={() => sendFunctionKey(number)}
        >F{number}</button>
      {/each}
    </div>
  {/if}
{/snippet}

{#snippet ctrlChordPopup()}
  {#if ctrlChordMenuOpen}
    <div class="ctrl-chord-popup" role="menu" aria-label="{chordSet.title} chords">
      <header>{chordSet.title}</header>
      {#each chordSet.chords as chord (chord.keys.join('+'))}
        <button
          role="menuitem"
          disabled={readOnly}
          onpointerdown={(event) => event.preventDefault()}
          onclick={() => sendCtrlChord(chord.keys, chord.label)}
        ><kbd>{chord.label}</kbd><span>{chord.hint}</span></button>
      {/each}
      <button
        role="menuitem"
        disabled={readOnly}
        onpointerdown={(event) => event.preventDefault()}
        onclick={armCtrlFromMenu}
      ><kbd>^ _</kbd><span>Arm Ctrl, type any key</span></button>
    </div>
  {/if}
{/snippet}

<main
  class:has-actions={inputLocked || nextBlocked}
  class:question-only={questionMode}
  class:find-open={findOpen}
  class:reader={readOnly}
  class="terminal-view"
  aria-label={`${questionMode ? 'Questions' : 'Terminal'} for ${agent.project || agent.name || agent.agent || 'agent'}`}
>
  {#if readOnly}
    <p class="key-feedback" role="status">Reader access is read only. Use a controller device to send input or answer prompts.</p>
  {/if}
  {#if questionMode && interaction}
    <QuestionForm {agent} {interaction} responding={responding.has(agent.pane_id)} />
    <div class="term-keys question-term-keys" aria-label="Terminal fallback keys" aria-busy={keySending}>
      <Button variant="secondary" size="sm" class={flashClass('esc')} onclick={() => sendTerminalKey('Escape', 'Cancelled prompt', 'esc')}>Esc</Button>
      <Button variant="secondary" size="sm" class={flashClass('tab')} aria-label="Tab" title="Send Tab" onclick={sendTab}>{@render tabIcon()}</Button>
      <div class="fkey-menu">
        <Button variant="secondary" size="sm" class={flashClass('fkeys')} aria-label="Function keys" aria-expanded={fkeysOpen} onclick={() => { fkeysOpen = !fkeysOpen; arrowsOpen = false; }}>
          F keys
        </Button>
      </div>
      <div class="arrow-menu">
        <Button variant="secondary" size="sm" aria-label="Arrow keys" aria-expanded={arrowsOpen} onclick={() => { arrowsOpen = !arrowsOpen; fkeysOpen = false; }}>
          {@render arrowIcon()}
        </Button>
      </div>
      <Button variant="secondary" size="sm" class={flashClass('enter')} aria-label="Enter" onclick={() => sendTerminalKey('Enter', 'Enter', 'enter')}>Enter</Button>
    </div>
  {/if}
  {#if questionMode && interaction}
    <div class="question-popups">
      {@render fkeyPopup()}
      {@render arrowPopup()}
      {@render ctrlChordPopup()}
    </div>
  {/if}
  <div class:hidden={questionMode} class="terminal-view term">
  {#if findOpen}
    <section class="terminal-find" aria-label="Find in terminal">
      <input
        bind:this={findInputElement}
        bind:value={findQuery}
        type="search"
        aria-label="Find in terminal output"
        placeholder="Find in terminal"
        autocomplete="off"
        autocapitalize="none"
        spellcheck="false"
        oninput={findInputChanged}
        onkeydown={findKeydown}
      />
      {#if findQuery.trim()}
        <span class="terminal-find-count" role="status" aria-live="polite">
          {#if !terminalFind.matches.length}
            No matches
          {:else}
            {activeFindIndex + 1} of {terminalFind.matches.length}{terminalFind.truncated ? '+' : ''}
          {/if}
        </span>
      {/if}
      <Button variant="secondary" size="sm" aria-label="Previous match" disabled={!terminalFind.matches.length} onclick={() => revealFindMatch(activeFindIndex - 1)}>
        {@render findPreviousIcon()}
      </Button>
      <Button variant="secondary" size="sm" aria-label="Next match" disabled={!terminalFind.matches.length} onclick={() => revealFindMatch(activeFindIndex + 1)}>
        {@render findNextIcon()}
      </Button>
      <Button variant="ghost" size="sm" aria-label="Close find" onclick={closeTerminalFind}>
        {@render findCloseIcon()}
      </Button>
    </section>
  {/if}
  <div class="term-wrap">
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
    <!-- Tap-to-focus only raises the software keyboard for the hidden capture
         field; the log itself stays a non-interactive scroll region. -->
  <div
    class:resize-layout={resizeLayoutActive} class:resize-pending={resizeLayoutPending} class:wide-view={wideView}
    class="term-content preserve-layout"
    data-copy-on-select
    bind:this={terminalElement}
    role="log"
    aria-label="Agent terminal output"
    onscroll={handleScroll}
    onpointerdown={() => { touchHeld = true; }}
    onpointerup={() => {
      touchHeld = false;
      releaseHeldScroll();
      if (terminalElement) {
        const gap = terminalElement.scrollHeight - terminalElement.scrollTop - terminalElement.clientHeight;
        virtualStickToBottom = gap < 48;
        jumpVisible = !virtualStickToBottom;
      }
    }}
    onpointercancel={() => {
      touchHeld = false;
      releaseHeldScroll();
      if (terminalElement) {
        const gap = terminalElement.scrollHeight - terminalElement.scrollTop - terminalElement.clientHeight;
        virtualStickToBottom = gap < 48;
        jumpVisible = !virtualStickToBottom;
      }
    }}
    onscrollcapture={syncWideGridScroll}
    onclick={terminalSurfaceClick}
  >
    <span
      bind:this={cellMeasureElement}
      aria-hidden="true"
      style="pointer-events: none; position: absolute; visibility: hidden; white-space: pre;"
    >{CELL_MEASURE_TEXT}</span>
    <div class="term-screen" bind:this={termScreenElement} data-terminal-row-count={renderedRows.length}>
      {#if virtualTopHeight > 0}
        <span class="terminal-virtual-spacer" style={`height:${virtualTopHeight}px`} aria-hidden="true"></span>
      {/if}
      <!-- Normalized rows are escaped before controlled ANSI spans enter this
           bounded DOM window. Keyed by index so a new frame patches rows in
           place: text selection, node identity, and scroll position survive. -->
      {#each virtualRows as row (row.index)}
        {@html row.html}
      {/each}
      {#if virtualBottomHeight > 0}
        <span class="terminal-virtual-spacer" style={`height:${virtualBottomHeight}px`} aria-hidden="true"></span>
      {/if}
    </div>
  </div>
    {#if jumpVisible}
      <button class="jump-bottom" aria-label="Jump to latest" onclick={jumpToBottom}>↓</button>
    {/if}
    {#if directInput}
      <textarea
        class="direct-capture"
        bind:this={directInputElement}
        aria-label="Direct terminal input"
        autocomplete="off"
        autocorrect="off"
        autocapitalize="none"
        spellcheck="false"
        enterkeyhint="send"
        tabindex="-1"
        rows="1"
        onkeydown={directKeydown}
        oninput={directInputEvent}
        oncompositionstart={directCompositionStart}
        oncompositionend={directCompositionEnd}
      ></textarea>
    {/if}
  </div>
  <textarea
    class="sr-only"
    aria-label="Full terminal transcript"
    readonly
    tabindex="-1"
    bind:this={transcriptElement}
    value={terminalPlainText}
  ></textarea>
  <textarea
    class="sr-only"
    aria-label="Latest final response"
    readonly
    tabindex="-1"
    bind:this={responseElement}
    value={terminalCopyText}
  ></textarea>
  {#if copiedAgentResponseText}
    <section class="agent-response-preview" aria-label="Copied agent response">
      <div class="agent-response-preview-header">
        <strong>Markdown response</strong>
        <div class="agent-response-preview-actions">
          <Button variant="secondary" size="sm" onclick={copyDisplayedAgentResponse}>Copy markdown</Button>
          <Button variant="ghost" size="sm" onclick={dismissCopiedAgentResponse}>Dismiss</Button>
        </div>
      </div>
      <textarea
        aria-label="Copied agent response markdown"
        readonly
        bind:this={agentResponsePreviewElement}
        value={copiedAgentResponseText}
      ></textarea>
    </section>
  {/if}
  <div class="terminal-copy">
    {#if !readOnly}
      <Button
        variant={directInput ? 'default' : 'secondary'}
        size="sm"
        aria-label={directInput ? 'Stop typing directly into the terminal' : 'Type directly into the terminal'}
        aria-pressed={directInput}
        title={directInput ? 'Stop direct typing' : 'Type directly into the terminal'}
        onclick={toggleDirectInput}
      >{@render keyboardIcon()}</Button>
    {/if}
    {#if resizeSessionActive}
      <Button
        variant={wideView ? 'default' : 'secondary'}
        size="sm"
        aria-label="Toggle wide view"
        aria-pressed={wideView}
        title="Wide view — lease the pane at 240 columns so ASCII diagrams and wide tables stay unwrapped; swipe sideways to pan"
        onclick={toggleWideView}
      >{@render wideIcon()}</Button>
    {/if}
    <Button
      variant="secondary"
      size="sm"
      aria-label={copyingAgentResponse ? 'Copying…' : 'Copy'}
      aria-busy={copyingAgentResponse}
      title={copyingAgentResponse ? 'Copying…' : 'Copy output'}
      disabled={copyingAgentResponse || responding.has(agent.pane_id)}
      onclick={copyTerminalOutput}
    >{@render copyIcon()}</Button>
    {#if $speechEnabled}
      <Button
        variant="secondary"
        size="sm"
        aria-label={$speechState === 'speaking' ? 'Stop reading response' : 'Read latest response aloud'}
        title={$speechState === 'speaking' ? 'Stop reading' : `Read latest response in ${speechLanguageLabel($speechLanguage)}`}
        aria-busy={fetchingSpeechText}
        disabled={fetchingSpeechText}
        onclick={() => { void speakTerminalResponse(); }}
      >{#if $speechState === 'speaking'}{@render stopIcon()}{:else}{@render speakerIcon()}{/if}</Button>
    {/if}
  </div>

  <div class="terminal-bottom" onfocusin={focusComposer} onfocusout={blurComposer}>
    {#if directInput}
      <div class="direct-input-bar" role="status">
        <span>Typing straight into the terminal — tap the screen to bring up the keyboard.</span>
        <Button variant="secondary" size="sm" onclick={toggleDirectInput}>Done</Button>
      </div>
    {/if}
    {#if slashMenuOpen}
      <section class="slash-command-popover" aria-label="Command suggestions">
        <header class="slash-command-header">
          <strong>Commands</strong>
          {#if !slashCatalogLoading && !slashCatalogUnavailable}
            <span>{filteredSlashCommands.length}{slashMatchesHidden ? '+' : ''} matching</span>
          {:else}
            <span>Type to filter</span>
          {/if}
        </header>
        {#if slashCatalog.status === 'partial'}
          <p class="slash-command-status" role="status">Runtime command discovery is incomplete. Loaded extensions and prompts may be missing; you can still send a command manually.</p>
        {:else if slashCatalog.status === 'loading'}
          <p class="slash-command-status" role="status">Pi is loading command resources. Refresh when loading finishes.</p>
        {:else if slashCatalog.status === 'unavailable'}
          <p class="slash-command-status" role="status">Runtime command discovery is unavailable. You can still send a command manually.</p>
        {/if}
        <button type="button" onclick={() => void refreshSlashCommands()} disabled={slashCatalogLoading}>
          Refresh commands
        </button>
        {#if slashCatalogLoading}
          <p class="slash-command-status" role="status">{slashCatalog.commands.length ? 'Refreshing commands…' : 'Loading commands…'}</p>
        {:else if slashCatalogUnavailable}
          <p class="slash-command-status" role="status">Suggestions unavailable — you can still send this command.</p>
        {:else if !filteredSlashCommands.length}
          <p class="slash-command-status" role="status">No matching command — you can still send it.</p>
        {/if}
        <div
          id="slash-command-options"
          class="slash-command-menu"
          role="listbox"
          aria-label="Slash commands"
          aria-busy={slashCatalogLoading}
        >
          {#each filteredSlashCommands as entry, index (entry.command)}
            <button
              id={`slash-command-option-${index}`}
              type="button"
              role="option"
              tabindex="-1"
              class:active={index === effectiveSlashIndex}
              aria-selected={index === effectiveSlashIndex}
              onpointerdown={(event) => event.preventDefault()}
              onpointerenter={() => { activeSlashIndex = index; }}
              onclick={() => selectSlashCommand(entry)}
            >
              <span class="slash-command-name">
                <strong>{entry.command}</strong>
                {#if entry.argument_hint}<small>{entry.argument_hint}</small>{/if}
              </span>
              <span class="slash-command-description">{entry.description}</span>
              {#if entry.source !== 'builtin'}<em class="slash-command-source">{entry.source}</em>{/if}
            </button>
          {/each}
        </div>
        {#if !slashCatalogLoading && slashCatalog.truncated}
          <p class="slash-command-limit" role="status">Command suggestions may be incomplete because a discovery limit was reached. Typing searches only loaded suggestions; you can still send a command manually.</p>
        {/if}
        {#if !slashCatalogLoading && slashMatchesHidden}
          <p class="slash-command-display-limit" role="status">More matching commands are hidden; keep typing to narrow the list.</p>
        {/if}
      </section>
    {/if}
    {#if noEchoActive}
      <section class="secret-prompt" aria-label="Hidden terminal prompt">
        <p id="secret-prompt-line" role="status">
          The terminal is asking for a hidden value: <strong>{noEchoPrompt}</strong>
        </p>
        {#if secretInputSupported}
          <div class="secret-prompt-row">
            <input
              bind:value={secretValue}
              type="password"
              aria-label="Value for the hidden terminal prompt"
              aria-describedby="secret-prompt-line"
              autocomplete="off"
              autocapitalize="none"
              spellcheck="false"
              enterkeyhint="send"
              disabled={readOnly || sendingSecret}
              onkeydown={secretKeydown}
            />
            <Button
              size="sm"
              disabled={readOnly || !secretValue || sendingSecret}
              aria-label="Send hidden value"
              onclick={submitSecret}
            >{sendingSecret ? '…' : 'Send'}</Button>
          </div>
          <p class="hint">Typed straight into the terminal: never saved on this phone and never written to activity.</p>
        {:else}
          <p class="hint">This computer’s relay is too old to accept a hidden value from the phone; answer it at the computer.</p>
        {/if}
      </section>
    {/if}
    <div class="term-input">
      <!-- Images get their own input: a mixed accept list makes Android offer
           the generic file picker instead of the photo picker, hiding
           screenshots behind a Files detour. -->
      <div class="attach-stack">
      <Button variant="ghost" size="icon" disabled={inputLocked || uploadingAttachment} aria-label="Attach photos" onclick={() => imageInput.click()}>
        <svg class="button-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
          <rect x="3" y="4" width="18" height="16" rx="2"></rect>
          <circle cx="8.5" cy="9" r="1.5"></circle>
          <path d="m4 17 4.5-4.5 3.5 3.5 2.5-2.5L20 19"></path>
        </svg>
      </Button>
      <Button variant="ghost" size="icon" disabled={inputLocked || uploadingAttachment} aria-label="Attach files" onclick={() => fileInput.click()}>
        <svg class="button-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
          <path d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 1 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48"></path>
        </svg>
      </Button>
      </div>
      <div class:awaiting-approval={approvalMode && !composerFocused} class:has-text={Boolean(composer)} class="composer-field">
        <textarea
          bind:this={composerElement}
          bind:value={composer}
          rows="1"
          disabled={composerLocked}
          readonly={$dictationState === 'listening'}
          inputmode={$dictationState === 'listening' ? 'none' : undefined}
          placeholder={approvalMode
            ? 'Approval pending — use buttons'
            : terminalTextMode
              ? terminalTextMode === 'filter' ? 'Type filter text…' : 'Type terminal input…'
              : inspectionMode
                ? 'Needs inspection — use terminal controls'
                : 'Type a reply…'}
          role="combobox"
          aria-label="Prompt"
          aria-autocomplete="list"
          aria-haspopup="listbox"
          aria-expanded={slashMenuOpen}
          aria-controls={slashMenuOpen ? 'slash-command-options' : undefined}
          aria-activedescendant={slashMenuOpen && effectiveSlashIndex >= 0 ? `slash-command-option-${effectiveSlashIndex}` : undefined}
          autocomplete="off"
          autocorrect="on"
          autocapitalize="sentences"
          spellcheck="true"
          enterkeyhint="enter"
          oninput={composerInput}
          onkeydown={keydown}
          onpaste={paste}
        ></textarea>
        {#if composer}<button class="input-clear" aria-label="Clear prompt text" onclick={clearComposer}>×</button>{/if}
      </div>
      <div class="send-stack">
      {#if dictationSupported}
        <Button
          variant={$dictationState === 'listening' ? 'default' : 'ghost'}
          size="icon"
          class={$dictationState === 'listening' ? 'listening' : undefined}
          disabled={composerLocked}
          aria-label={$dictationState === 'listening' ? 'Stop dictation' : 'Dictate prompt'}
          aria-pressed={$dictationState === 'listening'}
          title={$dictationState === 'listening' ? 'Stop dictation' : 'Dictate with the phone microphone'}
          onclick={toggleDictation}
        >
          <svg class="button-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
            <rect x="9" y="2" width="6" height="12" rx="3"></rect>
            <path d="M5 10a7 7 0 0 0 14 0"></path>
            <path d="M12 17v5"></path>
          </svg>
        </Button>
      {/if}
      <Button size="icon" disabled={!composer.replace(/[\r\n]+$/g, '') || composerLocked || sendingPrompt || uploadingAttachment} aria-label={sendingPrompt ? 'Submitting input' : terminalTextMode === 'filter' ? 'Send filter text' : inspectionMode ? 'Submit terminal text' : 'Send prompt'} onclick={sendPrompt}>{sendingPrompt ? '…' : '➤'}</Button>
      </div>
      <input bind:this={imageInput} type="file" accept="image/*" multiple hidden onchange={(event) => { void filesSelected(event.currentTarget.files || []); event.currentTarget.value = ''; }} />
      <input bind:this={fileInput} type="file" accept="image/png,image/jpeg,image/gif,image/webp,text/plain,text/markdown,text/csv,application/json,application/pdf,.docx,.xlsx,.pptx,.odt,.ods,.odp" multiple hidden onchange={(event) => { void filesSelected(event.currentTarget.files || []); event.currentTarget.value = ''; }} />
    </div>
    {#if attachmentSnapshot?.items.length}
      <AttachmentProgress snapshot={attachmentSnapshot} restartDisabled={inputLocked} oncancel={cancelAttachmentUpload} onrestart={restartAttachmentUpload} />
    {/if}
    {#if uploadStatus}<p class:error={uploadError} class="upload-status" role="status">{uploadStatus}</p>{/if}
    {#if draftPersistenceWarning}<p class="upload-status error" role="status">{draftPersistenceWarning}</p>{/if}
    {#if paneSizeLeaseError}<p class="upload-status error" role="alert">{paneSizeLeaseError}</p>{/if}
    {#if historyTruncated}
      <p class="upload-status" role="status">Older terminal history is not shown; this pane response was limited.</p>
    {/if}

    {#if visibleTerminalMenu}
      <section class="generic-menu-actions" aria-label={`Terminal menu: ${visibleTerminalMenu.title}`} aria-busy={keySending}>
        <header>
          <strong>{visibleTerminalMenu.title}</strong>
          <span>Detected from terminal key hints</span>
          <button type="button" aria-label="Dismiss detected menu actions" onclick={() => { dismissedMenuSignature = visibleTerminalMenu.signature; }}>×</button>
        </header>
        <div>
          {#each visibleTerminalMenu.actions as action (action.keys.join('+'))}
            <Button
              variant={action.cancel ? 'secondary' : 'default'}
              size="sm"
              disabled={readOnly}
              aria-label={action.label}
              onclick={() => { void sendKeys(action.keys, action.label); }}
            >{#if action.keys.length === 1 && action.keys[0] === 'Up'}{@render arrowUpIcon()}{:else if action.keys.length === 1 && action.keys[0] === 'Down'}{@render arrowDownIcon()}{:else if action.keys.length === 1 && action.keys[0] === 'Left'}{@render arrowLeftIcon()}{:else if action.keys.length === 1 && action.keys[0] === 'Right'}{@render arrowRightIcon()}{:else}<kbd>{menuKeyLabel(action.keys)}</kbd>{action.label}{/if}</Button>
          {/each}
        </div>
      </section>
    {/if}

    {#if approvalMode && !readOnly && !responding.has(agent.pane_id)}
      <div class="quick-actions" aria-label="Approval choices">
        {#each options as option, index (`${index}:${option}`)}
          <Button
            variant={approvalButtonTone(option, index, options.length) === 'deny' ? 'danger' : approvalButtonTone(option, index, options.length) === 'trust' ? 'trust' : 'default'}
            onclick={() => relayStore.respond(agent, index, options.length, option)}
          >{option}</Button>
        {/each}
        {#if nextBlocked}<Button variant="secondary" onclick={openNext}>Next blocked →</Button>{/if}
      </div>
    {:else if nextBlocked}
      <div class="quick-actions"><Button variant="secondary" onclick={openNext}>Next blocked →</Button></div>
    {/if}

    <div class="term-keys" aria-busy={keySending}>
      <Button variant="secondary" size="sm" disabled={readOnly} class={flashClass('esc')} onpointerdown={(event) => event.preventDefault()} onclick={() => sendTerminalKey('Escape', 'Cancelled prompt', 'esc')}>Esc</Button>
      <Button variant="secondary" size="sm" disabled={readOnly} class={flashClass('tab')} aria-label="Tab" title="Send Tab" onpointerdown={(event) => event.preventDefault()} onclick={sendTab}>{@render tabIcon()}</Button>
      <div class="modifier-menu">
        <input
          id="modifier-key-input"
          class="modifier-key-input"
          bind:this={modifierInputElement}
          disabled={readOnly || sendingFilter}
          aria-label="Modifier shortcut character"
          autocomplete="off"
          autocapitalize="none"
          maxlength="1"
          spellcheck="false"
          oninput={modifierInput}
          onkeydown={modifierKeydown}
          onblur={modifierBlur}
        />
        <Button
          variant="secondary"
          size="sm"
          disabled={readOnly}
          aria-controls="modifier-key-input"
          aria-pressed={shiftArmed}
          aria-label="Shift"
          title="Arm Shift; combine it with Ctrl or Alt"
          onpointerdown={(event) => event.preventDefault()}
          onclick={toggleShift}
        >{@render shiftIcon()}</Button>
        <Button
          variant="secondary"
          size="sm"
          disabled={readOnly}
          class={flashClass('ctrl')}
          aria-controls="modifier-key-input"
          aria-pressed={ctrlArmed}
          aria-expanded={ctrlChordMenuOpen}
          aria-label="Ctrl chords"
          title="Common Ctrl chords; arm Ctrl to type your own"
          onpointerdown={(event) => event.preventDefault()}
          onclick={toggleCtrlMenu}
        ><span class="key-caret">^</span></Button>
        <Button
          variant="secondary"
          size="sm"
          disabled={readOnly}
          aria-controls="modifier-key-input"
          aria-pressed={altArmed}
          title="Arm Alt; combine it with Ctrl or Shift"
          onpointerdown={(event) => event.preventDefault()}
          onclick={toggleAlt}
        >Alt</Button>
      </div>
      <div class="fkey-menu">
        <Button
          variant="secondary"
          size="sm"
          disabled={readOnly}
          class={flashClass('fkeys')}
          aria-label="Function keys"
          aria-expanded={fkeysOpen}
          onpointerdown={(event) => event.preventDefault()}
          onclick={() => { fkeysOpen = !fkeysOpen; arrowsOpen = false; ctrlChordMenuOpen = false; }}
        >F keys</Button>
      </div>
      <div class="arrow-menu">
        <Button
          variant="secondary"
          size="sm"
          disabled={readOnly}
          aria-label="Arrow keys"
          aria-expanded={arrowsOpen}
          onpointerdown={(event) => event.preventDefault()}
          onclick={() => { arrowsOpen = !arrowsOpen; fkeysOpen = false; ctrlChordMenuOpen = false; }}
        >
          {@render arrowIcon()}
        </Button>
      </div>
      <Button variant="secondary" size="sm" disabled={readOnly || keySending || sendingPrompt} aria-label="Ctrl+Enter — queue the typed follow-up" title="Queue the typed text (Ctrl+Enter)" onpointerdown={(event) => event.preventDefault()} onclick={() => { void sendCtrlEnter(); }}>{@render queueIcon()}</Button>
      <Button variant="secondary" size="sm" disabled={readOnly} class={flashClass('enter')} aria-label="Enter" onpointerdown={(event) => event.preventDefault()} onclick={() => sendTerminalKey('Enter', 'Enter', 'enter')}>Enter</Button>
    </div>

    <!-- Popups live outside the scrollable .term-keys: overflow-x:auto would
         clip them vertically and they'd never paint. Anchored to
         .terminal-bottom (position:relative) instead. -->
    {@render fkeyPopup()}
    {@render arrowPopup()}
    {@render ctrlChordPopup()}
  </div>
</div>
</main>
