<script lang="ts">
  /**
   * Dark-themed select replacement: the native <select> popup renders with
   * the OS's own styling (light gray on Android), which clashes with the
   * app's dark surface. Same contract as a select: value in, change out.
   */
  type Option = { value: string; label: string };

  type Props = {
    id?: string;
    options: Option[];
    value: string;
    disabled?: boolean;
    placeholder?: string;
    'aria-label'?: string;
    onchange?: (value: string) => void;
  };

  let {
    id,
    options,
    value = $bindable(''),
    disabled = false,
    placeholder = 'Select…',
    onchange,
    ...rest
  }: Props = $props();

  let open = $state(false);
  let root = $state<HTMLDivElement>(null!);
  let activeIndex = $state(-1);

  const selected = $derived(options.find((option) => option.value === value));

  function toggle() {
    if (disabled) return;
    open = !open;
    if (open) activeIndex = Math.max(0, options.findIndex((option) => option.value === value));
  }

  function choose(option: Option) {
    value = option.value;
    open = false;
    onchange?.(option.value);
  }

  function onKeydown(event: KeyboardEvent) {
    if (disabled) return;
    if (!open) {
      if (event.key === 'Enter' || event.key === ' ' || event.key === 'ArrowDown') {
        event.preventDefault();
        toggle();
      }
      return;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      open = false;
    } else if (event.key === 'ArrowDown') {
      event.preventDefault();
      activeIndex = Math.min(options.length - 1, activeIndex + 1);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      activeIndex = Math.max(0, activeIndex - 1);
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      if (options[activeIndex]) choose(options[activeIndex]);
    }
  }

  function onFocusOut(event: FocusEvent) {
    if (!root.contains(event.relatedTarget as Node)) open = false;
  }
</script>

<div
  bind:this={root}
  class="app-select"
  class:open
  class:disabled
  onfocusout={onFocusOut}
>
  <button
    {id}
    type="button"
    class="app-select-trigger"
    {disabled}
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-label={rest['aria-label']}
    onclick={toggle}
    onkeydown={onKeydown}
  >
    <span class="app-select-value" class:placeholder={!selected}>{selected?.label || placeholder}</span>
    <svg class="app-select-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="m6 9 6 6 6-6"></path></svg>
  </button>
  {#if open}
    <ul class="app-select-list" role="listbox" aria-label={rest['aria-label']} tabindex="-1">
      {#each options as option, index (option.value)}
        <li
          role="option"
          aria-selected={option.value === value}
          class:active={index === activeIndex}
          class="app-select-option"
          onpointerdown={(event) => { event.preventDefault(); choose(option); }}
          onpointerenter={() => { activeIndex = index; }}
        >
          <span>{option.label}</span>
          {#if option.value === value}
            <svg class="app-select-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="m5 13 4 4L19 7"></path></svg>
          {/if}
        </li>
      {/each}
      {#if !options.length}
        <li class="app-select-option app-select-empty" role="option" aria-selected="false" aria-disabled="true">{placeholder}</li>
      {/if}
    </ul>
  {/if}
</div>
