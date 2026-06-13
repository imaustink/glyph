<script lang="ts">
  import {
    startOfMonth,
    endOfMonth,
    startOfWeek,
    endOfWeek,
    addMonths,
    subMonths,
    eachDayOfInterval,
    format,
    isSameMonth,
    isSameDay,
    isToday,
    parseISO
  } from 'date-fns';

  let {
    value = null,
    onchange,
    id
  }: {
    value: string | null;
    onchange: (value: string | null) => void;
    id?: string;
  } = $props();

  let open = $state(false);
  let viewDate = $state(new Date());
  let triggerEl: HTMLButtonElement | undefined = $state();
  let dropdownEl: HTMLDivElement | undefined = $state();

  const selectedDate = $derived(value ? parseISO(value) : null);

  const calendarDays = $derived.by(() => {
    const monthStart = startOfMonth(viewDate);
    const monthEnd = endOfMonth(viewDate);
    const calStart = startOfWeek(monthStart, { weekStartsOn: 0 });
    const calEnd = endOfWeek(monthEnd, { weekStartsOn: 0 });
    return eachDayOfInterval({ start: calStart, end: calEnd });
  });

  const displayValue = $derived(
    selectedDate ? format(selectedDate, 'MMM d, yyyy') : ''
  );

  const monthLabel = $derived(format(viewDate, 'MMMM yyyy'));

  function toggle() {
    if (!open && selectedDate) {
      viewDate = selectedDate;
    } else if (!open) {
      viewDate = new Date();
    }
    open = !open;
  }

  function prevMonth() {
    viewDate = subMonths(viewDate, 1);
  }

  function nextMonth() {
    viewDate = addMonths(viewDate, 1);
  }

  function selectDay(day: Date) {
    onchange(format(day, 'yyyy-MM-dd'));
    open = false;
  }

  function clear() {
    onchange(null);
    open = false;
  }

  function selectToday() {
    onchange(format(new Date(), 'yyyy-MM-dd'));
    open = false;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      open = false;
      triggerEl?.focus();
    }
  }

  function handleWindowClick(e: MouseEvent) {
    if (
      open &&
      triggerEl &&
      dropdownEl &&
      !triggerEl.contains(e.target as Node) &&
      !dropdownEl.contains(e.target as Node)
    ) {
      open = false;
    }
  }

  const WEEKDAYS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];
</script>

<svelte:window onclick={handleWindowClick} onkeydown={handleKeydown} />

<div class="datepicker" class:open>
  <button
    {id}
    type="button"
    class="datepicker-trigger"
    bind:this={triggerEl}
    onclick={toggle}
    aria-haspopup="dialog"
    aria-expanded={open}
  >
    {#if displayValue}
      <span class="datepicker-value">{displayValue}</span>
    {:else}
      <span class="datepicker-placeholder">Pick a date…</span>
    {/if}
    <svg class="datepicker-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <rect x="3" y="4" width="18" height="18" rx="2" ry="2"/>
      <line x1="16" y1="2" x2="16" y2="6"/>
      <line x1="8" y1="2" x2="8" y2="6"/>
      <line x1="3" y1="10" x2="21" y2="10"/>
    </svg>
  </button>

  {#if open}
    <div class="datepicker-dropdown" bind:this={dropdownEl} role="dialog" aria-label="Choose date">
      <div class="dp-header">
        <button type="button" class="dp-nav-btn" onclick={prevMonth} aria-label="Previous month">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
            <polyline points="15 18 9 12 15 6"/>
          </svg>
        </button>
        <span class="dp-month-label">{monthLabel}</span>
        <button type="button" class="dp-nav-btn" onclick={nextMonth} aria-label="Next month">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
            <polyline points="9 18 15 12 9 6"/>
          </svg>
        </button>
      </div>

      <div class="dp-weekdays">
        {#each WEEKDAYS as day}
          <span class="dp-weekday">{day}</span>
        {/each}
      </div>

      <div class="dp-days">
        {#each calendarDays as day}
          <button
            type="button"
            class="dp-day"
            class:outside={!isSameMonth(day, viewDate)}
            class:today={isToday(day)}
            class:selected={selectedDate !== null && isSameDay(day, selectedDate)}
            onclick={() => selectDay(day)}
          >
            {format(day, 'd')}
          </button>
        {/each}
      </div>

      <div class="dp-footer">
        <button type="button" class="dp-footer-btn" onclick={selectToday}>Today</button>
        {#if value}
          <button type="button" class="dp-footer-btn dp-clear" onclick={clear}>Clear</button>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .datepicker {
    position: relative;
    display: inline-block;
    width: 100%;
  }

  .datepicker.open {
    z-index: 100;
  }

  .datepicker-trigger {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    width: 100%;
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    padding: 4px 8px;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    cursor: pointer;
    text-align: left;
    transition: border-color var(--transition-fast), background var(--transition-fast);
  }

  .datepicker-trigger:hover {
    border-color: var(--border-default);
  }

  .datepicker-trigger:focus {
    border-color: var(--accent);
    background: var(--bg-tertiary);
    outline: none;
  }

  .datepicker-placeholder {
    color: var(--text-muted);
  }

  .datepicker-icon {
    flex-shrink: 0;
    color: var(--text-muted);
  }

  .datepicker-dropdown {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    z-index: 100;
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 12px;
    width: 300px;
    user-select: none;
  }

  .dp-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 10px;
  }

  .dp-month-label {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--text-heading);
  }

  .dp-nav-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--text-secondary);
    padding: 4px;
    cursor: pointer;
    transition: color var(--transition-fast), background var(--transition-fast);
  }

  .dp-nav-btn:hover {
    color: var(--text-primary);
    background: var(--bg-hover);
  }

  .dp-weekdays {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    margin-bottom: 4px;
  }

  .dp-weekday {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    text-align: center;
    padding: 2px 0;
    font-weight: 500;
  }

  .dp-days {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    gap: 2px;
  }

  .dp-day {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    aspect-ratio: 1;
    padding: 0;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast);
  }

  .dp-day:hover {
    background: var(--bg-hover);
  }

  .dp-day.outside {
    color: var(--text-muted);
    opacity: 0.4;
  }

  .dp-day.today {
    color: var(--accent);
    font-weight: 600;
  }

  .dp-day.selected {
    background: var(--accent);
    color: #fff;
    font-weight: 600;
  }

  .dp-day.selected:hover {
    background: var(--accent-hover);
  }

  .dp-footer {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--border-subtle);
  }

  .dp-footer-btn {
    font-size: var(--font-size-xs);
    color: var(--accent);
    background: transparent;
    border: none;
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .dp-footer-btn:hover {
    background: var(--accent-muted);
  }

  .dp-clear {
    color: var(--text-muted);
  }

  .dp-clear:hover {
    color: var(--text-secondary);
    background: var(--bg-hover);
  }
</style>
