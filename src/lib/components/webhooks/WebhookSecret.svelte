<script lang="ts">
  let { secret }: { secret: string } = $props();
  let copied = $state(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(secret);
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch {
      /* clipboard unavailable — the secret can still be selected manually */
    }
  }
</script>

<div class="secret">
  <code>{secret}</code>
  <button type="button" onclick={copy}>{copied ? 'Copied' : 'Copy'}</button>
</div>

<style>
  .secret {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  code {
    flex: 1;
    min-width: 0;
    padding: 6px 8px;
    border-radius: var(--radius-md);
    background: var(--bg-tertiary);
    color: var(--text-primary);
    font-size: var(--font-size-xs);
    overflow-wrap: anywhere;
    user-select: all;
  }

  button {
    padding: 6px 12px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border-default);
    background: none;
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }
</style>
