<script lang="ts">
  import AppSwitch from '$components/ui/AppSwitch.svelte';
  import AppSelect from '$components/ui/AppSelect.svelte';
  import Button from '$components/ui/Button.svelte';
  import Card from '$components/ui/Card.svelte';
  import {
    clearSnooze,
    pushPolicyScopeKey,
    withGlobalSnooze,
    withTimedSnooze,
    type DevicePushPolicy,
    type NotificationPlatformInfo,
    type NotificationPolicyScope,
    type PushCategory,
    type PushPolicyChange,
    type PushTestRequest,
    type PushTestState,
  } from '$lib/push-policy';

  type ConfigurableCategory = Exclude<PushCategory, 'brief' | 'update'>;
  const CATEGORY_LABELS: Record<ConfigurableCategory, { label: string; detail: string }> = {
    attention: { label: 'Approval needed', detail: 'An agent needs your review.' },
    question: { label: 'Questions', detail: 'An agent needs your answer.' },
    finished: { label: 'Finished', detail: 'An agent finished.' },
    test: { label: 'Test notifications', detail: 'Delivery checks for this device.' },
  };
  const CONFIGURABLE_CATEGORIES: ConfigurableCategory[] = ['attention', 'question', 'finished', 'test'];

  let {
    scopes = [],
    platform,
    testState = { status: 'idle' },
    testStates = {},
    busy = false,
    deliveryEnabled = false,
    onpolicychange,
    ontoggle,
    ontest,
  }: {
    scopes?: NotificationPolicyScope[];
    platform: NotificationPlatformInfo;
    testState?: PushTestState;
    testStates?: Record<string, PushTestState>;
    busy?: boolean;
    // Categories, delays, and tests only mean something once this browser
    // subscribes, so they stay hidden until push delivery is on.
    deliveryEnabled?: boolean;
    onpolicychange?: (change: PushPolicyChange) => void | Promise<void>;
    ontoggle?: () => void | Promise<void>;
    ontest?: (request: PushTestRequest) => void | Promise<void>;
  } = $props();

  let selectedKey = $state('');
  let localPolicies = $state<Record<string, DevicePushPolicy>>({});
  let saving = $state(false);
  let policyError = $state('');

  const selectedScope = $derived.by(() => {
    const exact = scopes.find(scope => pushPolicyScopeKey(scope.relay_id, scope.device_id) === selectedKey);
    return exact || scopes[0] || null;
  });
  const effectiveSelectedKey = $derived(selectedScope
    ? pushPolicyScopeKey(selectedScope.relay_id, selectedScope.device_id)
    : '');
  const selectedPolicy = $derived(selectedScope
    ? localPolicies[effectiveSelectedKey] || selectedScope.policy
    : null);
  const selectedTestState = $derived(selectedScope
    ? testStates[selectedScope.relay_id] || testState
    : testState);
  const blocked = $derived(!platform.supports_push || platform.permission === 'denied');
  const deliveryHint = $derived.by(() => {
    if (!platform.supports_push) return 'This browser cannot receive push notifications.';
    if (platform.permission === 'denied') return 'Notifications are blocked for this site. Allow them below, then enable delivery again.';
    if (deliveryEnabled) return 'This browser keeps per-relay subscriptions synchronized.';
    return 'Enable delivery before testing or receiving background notifications.';
  });

  function chooseScope(value: string): void {
    selectedKey = value;
  }

  async function applyPolicy(policy: DevicePushPolicy): Promise<void> {
    if (!selectedScope || !selectedPolicy || saving) return;
    const key = effectiveSelectedKey;
    const previous = selectedPolicy;
    localPolicies = { ...localPolicies, [key]: policy };
    policyError = '';
    saving = true;
    try {
      await onpolicychange?.({
        relay_id: selectedScope.relay_id,
        device_id: selectedScope.device_id,
        policy,
      });
    } catch {
      localPolicies = { ...localPolicies, [key]: previous };
      policyError = 'The relay did not save this notification policy.';
    } finally {
      saving = false;
    }
  }

  function changeCategory(category: PushCategory, checked: boolean): void {
    if (!selectedPolicy) return;
    applyPolicy({
      ...selectedPolicy,
      categories: { ...selectedPolicy.categories, [category]: checked },
    });
  }

  function changeMilliseconds(field: 'settle_ms' | 'cooldown_ms', value: string): void {
    if (!selectedPolicy) return;
    applyPolicy({ ...selectedPolicy, [field]: Number(value) });
  }

  function changeSnooze(duration: string): void {
    if (!selectedPolicy) return;
    if (duration === 'global') applyPolicy(withGlobalSnooze(selectedPolicy));
    else if (duration === 'off') applyPolicy(clearSnooze(selectedPolicy));
    else applyPolicy(withTimedSnooze(selectedPolicy, Number(duration)));
  }

  function snoozeSelection(policy: DevicePushPolicy): string {
    if (!policy.snoozed) return 'off';
    return policy.snooze_until ? 'timed' : 'global';
  }

  function sendTest(): void {
    if (!selectedScope?.current_device) return;
    void ontest?.({ relay_id: selectedScope.relay_id });
  }

  const testMessage = $derived.by(() => {
    if (selectedTestState.status === 'sending') return 'Asking the relay service to send a neutral test…';
    if (selectedTestState.status === 'accepted') {
      const verb = selectedTestState.result === 'queued' ? 'queued' : 'accepted';
      return `The relay service ${verb} the test. This does not confirm that the phone displayed it.`;
    }
    if (selectedTestState.status === 'rejected') return `The relay could not accept the test (${selectedTestState.code}).`;
    return 'A test reports relay service acceptance only; it cannot confirm handset display or human delivery.';
  });
</script>

<Card aria-labelledby="notification-settings-title">
  <h3 id="notification-settings-title">Notifications</h3>
  <Button
    disabled={busy || !platform.supports_push}
    onclick={() => void ontoggle?.()}
  >{deliveryEnabled ? 'Stop Push Notifications' : 'Enable Push Notifications'}</Button>
  <p class="hint" role="status">{deliveryHint}</p>

  {#if deliveryEnabled}
    {#if scopes.length > 0 && selectedScope && selectedPolicy}
      <label class="field-label scope-label" for="notification-scope">Relay and device</label>
      <AppSelect
        id="notification-scope"
        options={scopes.map((scope) => ({
          value: pushPolicyScopeKey(scope.relay_id, scope.device_id),
          label: `${scope.relay_label} — ${scope.device_label}${scope.current_device ? ' (this device)' : ''}`,
        }))}
        value={effectiveSelectedKey}
        disabled={busy || saving}
        aria-label="Relay and device"
        onchange={chooseScope}
      />

      <fieldset aria-label="Notification categories" disabled={busy || saving}>
        {#each CONFIGURABLE_CATEGORIES as category (category)}
          <div class="setting-row">
            <AppSwitch
              checked={selectedPolicy.categories[category]}
              label={CATEGORY_LABELS[category].label}
              descriptionId={`push-category-${category}`}
              onchange={checked => changeCategory(category, checked)}
            />
            <p id={`push-category-${category}`} class="hint">{CATEGORY_LABELS[category].detail}</p>
          </div>
        {/each}
      </fieldset>

      <div class="grid">
        <label>
          Settle delay
          <AppSelect
            options={[
              { value: '0', label: 'Immediately' },
              { value: '2000', label: '2 seconds' },
              { value: '5000', label: '5 seconds' },
              { value: '15000', label: '15 seconds' },
            ]}
            value={String(selectedPolicy.settle_ms)}
            disabled={busy || saving}
            aria-label="Settle delay"
            onchange={(value) => changeMilliseconds('settle_ms', value)}
          />
        </label>
        <label>
          Cooldown
          <AppSelect
            options={[
              { value: '0', label: 'None' },
              { value: '30000', label: '30 seconds' },
              { value: '60000', label: '1 minute' },
              { value: '300000', label: '5 minutes' },
            ]}
            value={String(selectedPolicy.cooldown_ms)}
            disabled={busy || saving}
            aria-label="Cooldown"
            onchange={(value) => changeMilliseconds('cooldown_ms', value)}
          />
        </label>
        <label>
          Snooze
          <AppSelect
            options={[
              { value: 'off', label: 'Not snoozed' },
              ...(selectedPolicy.snoozed && selectedPolicy.snooze_until
                ? [{ value: 'timed', label: `Until ${new Date(selectedPolicy.snooze_until).toLocaleString()}` }]
                : []),
              { value: '3600000', label: 'For 1 hour' },
              { value: '28800000', label: 'For 8 hours' },
              { value: '86400000', label: 'For 24 hours' },
              { value: 'global', label: 'Until I turn it back on' },
            ]}
            value={snoozeSelection(selectedPolicy)}
            disabled={busy || saving}
            aria-label="Snooze"
            onchange={changeSnooze}
          />
        </label>
      </div>

      {#if policyError}<p class="hint error" role="alert">{policyError}</p>{/if}

      <div class="test-row">
        <Button
          variant="secondary"
          size="sm"
          disabled={busy || saving || selectedTestState.status === 'sending' || !selectedScope.current_device}
          onclick={sendTest}
        >Send neutral test</Button>
        {#if !selectedScope.current_device}
          <p class="hint">Test notifications from that device.</p>
        {/if}
      </div>
      <p class="hint" aria-live="polite">{testMessage}</p>
    {:else}
      <p class="hint">No paired notification device is available.</p>
    {/if}
  {/if}

  {#if platform.platform === 'ios' && !platform.installed}
    <section class="guidance" aria-labelledby="ios-install-title">
      <h4 id="ios-install-title">Install on iPhone or iPad first</h4>
      <ol>
        <li>Open this site in Safari on iOS or iPadOS 16.4+.</li>
        <li>Tap Share, Add to Home Screen, then Add.</li>
        <li>Open Herdr from the Home Screen and return here.</li>
      </ol>
      <p class="hint">iOS allows Web Push only from an installed Home Screen app after a tap.</p>
    </section>
  {/if}

  {#if blocked}
    <section class="guidance" aria-labelledby="manual-settings-title">
      <h4 id="manual-settings-title">Allow notifications for this app</h4>
      {#if platform.platform === 'ios'}
        <p class="hint"><strong>iPhone or iPad:</strong> In Settings, open Notifications, choose Herdr, and enable Allow Notifications.</p>
      {:else if platform.platform === 'android'}
        <p class="hint"><strong>Android:</strong> Long-press Herdr, open App info, then Notifications. For a browser tab, use its site settings.</p>
      {:else}
        <p class="hint">Use your browser's site permissions and your system's notification settings.</p>
      {/if}
    </section>
  {/if}
</Card>

<style>
  h4 { font-size: .82rem; margin: 0 0 .35rem; }
  .scope-label { display: block; margin: .9rem 0 .3rem; }
  fieldset { border: 0; margin: 1rem 0 0; padding: 0; }
  .setting-row { border-top: 1px solid var(--border); padding: .7rem 0 .45rem; }
  .setting-row .hint { margin: .25rem 0 0; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(9rem, 1fr)); gap: .7rem; margin-top: 1rem; }
  .grid label { display: block; font-size: .78rem; font-weight: 650; }
  .grid :global(.app-select) { margin-top: .3rem; }
  .test-row { display: flex; align-items: center; gap: .7rem; margin-top: .9rem; }
  .test-row .hint { margin: 0; }
  .guidance { border-top: 1px solid var(--border); margin-top: .9rem; padding-top: .9rem; }
  .guidance ol { font-size: .8rem; line-height: 1.5; margin: .35rem 0 .5rem; padding-left: 1.35rem; }
</style>
