<script>
    // In-app notifications (approvals, overdue reminders, bookings, prints).
    // There is no email, so this bell is how people hear back.
    import { onMount } from 'svelte';
    import { api } from '$lib/auth.js';
    import IconBell from '@tabler/icons-svelte-runes/icons/bell';

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

    function outside(e) {
        if (open && !e.target.closest?.('.bell')) open = false;
    }

    const when = (d) => new Date(d).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
</script>

<svelte:window on:click={outside} on:keydown={(e) => e.key === 'Escape' && (open = false)} />

<div class="nav-item dropdown bell">
    <button
        class="btn btn-ghost-secondary btn-icon position-relative"
        type="button"
        on:click={toggle}
        aria-expanded={open}
        aria-label="Notifications{unread ? `, ${unread} unread` : ''}"
    >
        <IconBell size={20} />
        {#if unread}<span class="badge bg-danger badge-notification badge-pill">{unread}</span>{/if}
    </button>
    <div class="dropdown-menu dropdown-menu-end dropdown-menu-arrow dropdown-menu-card" class:show={open}>
        <div class="card">
            <div class="card-header">
                <h3 class="card-title">Notifications</h3>
            </div>
            <div class="list-group list-group-flush list-group-hoverable">
                {#if notes.length === 0}
                    <div class="list-group-item text-secondary">You're all caught up.</div>
                {/if}
                {#each notes as n (n.id)}
                    <a class="list-group-item list-group-item-action" href={n.link || '#'} on:click={() => (open = false)}>
                        <div class="d-flex gap-2">
                            <span class="status-dot mt-2 flex-shrink-0" class:status-dot-animated={!n.read_at} class:bg-primary={!n.read_at} class:bg-transparent={n.read_at}></span>
                            <div class="min-w-0">
                                <div class="text-body text-wrap">{n.text}</div>
                                <div class="small text-secondary mt-1">{when(n.created_at)}</div>
                            </div>
                        </div>
                    </a>
                {/each}
            </div>
        </div>
    </div>
</div>

<style>
    .bell {
        position: relative;
    }

    .dropdown-menu {
        position: absolute;
        right: -3rem;
        left: auto;
        top: calc(100% + 0.5rem);
        width: min(380px, calc(100vw - 24px));
    }

    .list-group {
        max-height: min(60vh, 460px);
        overflow-y: auto;
    }

    .badge-notification {
        position: absolute;
        top: 2px;
        right: 2px;
        font-size: 0.65rem;
        min-width: 1.1rem;
        padding: 0.15rem 0.3rem;
    }
</style>
