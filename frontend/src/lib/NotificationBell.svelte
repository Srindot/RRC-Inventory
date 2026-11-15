<script>
    // In-app notifications (approvals, overdue reminders, bookings). There
    // is no email, so this bell is how people hear back.
    import { onMount } from 'svelte';
    import { api } from '$lib/auth.js';
    import Icon from '$lib/Icon.svelte';

    export let up = false; // open upwards, for the sidebar footer

    let notes = [];
    let open = false;

    $: unread = notes.filter((n) => !n.read_at).length;

    onMount(async () => {
        const response = await api('/api/notifications').catch(() => null);
        if (response?.ok) notes = await response.json();
    });

    async function toggle() {
        open = !open;
        if (open && unread > 0) {
            await api('/api/notifications/read', { method: 'POST' }).catch(() => null);
            const now = new Date().toISOString();
            setTimeout(() => (notes = notes.map((n) => ({ ...n, read_at: n.read_at || now }))), 1500);
        }
    }

    const when = (d) => new Date(d).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
</script>

<svelte:window on:keydown={(e) => e.key === 'Escape' && (open = false)} />

<div class="bell">
    <button class="bell-btn" on:click={toggle} aria-expanded={open} aria-label="Notifications{unread ? `, ${unread} unread` : ''}">
        <Icon name="bell" />
        {#if unread}<span class="count">{unread}</span>{/if}
    </button>
    {#if open}
        <div class="dropdown" class:up>
            <p class="head">Notifications</p>
            {#if notes.length === 0}
                <p class="empty">You're all caught up.</p>
            {/if}
            {#each notes as n (n.id)}
                <a class="note" class:unread={!n.read_at} href={n.link || '#'} on:click={() => (open = false)}>
                    <span>{n.text}</span>
                    <small class="mono">{when(n.created_at)}</small>
                </a>
            {/each}
        </div>
    {/if}
</div>

<style>
    .bell {
        position: relative;
    }

    .bell-btn {
        position: relative;
        display: grid;
        place-items: center;
        width: 40px;
        height: 40px;
        border: none;
        border-radius: var(--radius-sm);
        background: transparent;
        color: var(--muted);
        cursor: pointer;
    }

    .bell-btn:hover {
        background: var(--line);
        color: var(--ctp-text);
    }

    .count {
        position: absolute;
        top: 4px;
        right: 2px;
        min-width: 16px;
        padding: 0 4px;
        border-radius: var(--radius-pill);
        background: var(--accent);
        color: var(--accent-ink);
        font-size: 0.68rem;
        font-weight: 700;
        line-height: 16px;
    }

    .dropdown {
        position: absolute;
        right: 0;
        top: calc(100% + 6px);
        width: 340px;
        max-height: 60vh;
        overflow-y: auto;
        background: var(--card-raised);
        border: 1px solid var(--line-strong);
        border-radius: var(--radius);
        box-shadow: var(--shadow);
        z-index: 80;
    }

    .dropdown.up {
        top: auto;
        bottom: calc(100% + 6px);
        right: auto;
        left: 0;
    }

    /* On phones anchor the list to the screen, wherever the bell sits */
    @media (max-width: 899px) {
        .dropdown,
        .dropdown.up {
            position: fixed;
            left: 16px;
            right: 16px;
            top: 64px;
            bottom: auto;
            width: auto;
        }
    }

    .head {
        margin: 0;
        padding: 12px 14px 8px;
        font-size: 0.8rem;
        font-weight: 600;
        color: var(--muted);
    }

    .note {
        display: flex;
        flex-direction: column;
        gap: 2px;
        padding: 10px 14px;
        border-top: 1px solid var(--line);
        color: var(--muted);
        text-decoration: none;
        font-size: 0.88rem;
    }

    .note.unread {
        color: var(--ctp-text);
    }

    .note:hover {
        background: var(--line);
    }

    .note small {
        color: var(--faint);
        font-size: 0.75rem;
    }

    .empty {
        margin: 0;
        padding: 6px 14px 14px;
        color: var(--faint);
        font-size: 0.88rem;
    }
</style>
