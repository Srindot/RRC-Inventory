<script>
    // What's in the lab: every item, who has it right now, and any reported
    // problem. People keep it accurate as they go - handing gear on, flagging
    // issues - so nobody has to run a stocktake.
    import { onMount } from 'svelte';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import Icon from '$lib/Icon.svelte';
    import { AREA, LAB } from '$lib/palette.js';

    // Same lab colours as the admin page tabs

    let user = null;
    let items = [];
    let pendingLoans = [];
    let query = '';
    let loading = true;
    let message = '';
    let messageType = '';
    let issueFor = null;
    let issueText = '';

    $: shown = items.filter((i) => {
        const q = query.trim().toLowerCase();
        return !q || i.name.toLowerCase().includes(q) || i.home_lab.toLowerCase().includes(q) ||
               i.out.some((l) => l.borrower_name.toLowerCase().includes(q));
    });

    onMount(async () => {
        user = getUser();
        load();
        user = await refreshUser();
        if (can(user, 'approve_loans')) loadPending();
    });

    async function load() {
        loading = true;
        try {
            const response = await fetch('/api/items');
            if (response.ok) items = await response.json();
            else show('Could not load the catalogue', 'error');
        } catch {
            show('Could not reach the server', 'error');
        } finally {
            loading = false;
        }
    }

    async function loadPending() {
        const response = await api('/api/loans/pending').catch(() => null);
        if (response?.ok) pendingLoans = await response.json();
    }

    async function post(path, body, after) {
        if (!user) {
            location.href = loginUrl();
            return;
        }
        try {
            const response = await api(path, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body || {})
            });
            if (response.status === 401) {
                location.href = loginUrl();
                return;
            }
            const result = await response.json().catch(() => ({}));
            show(result.message || result.error || `HTTP ${response.status}`, response.ok ? 'success' : 'error');
            if (response.ok) after?.();
        } catch {
            show('Network error. Please try again.', 'error');
        }
    }

    function takeOver(item, loan) {
        if (confirm(`Take ${item.name} over from ${loan.borrower_name}? It moves to your account and you're responsible for it.`)) {
            post(`/api/loans/${loan.ID}/handover`, { accept: true }, load);
        }
    }

    function saveIssue(item, text) {
        post(`/api/items/${item.ID}/issue`, { issue: text }, () => {
            issueFor = null;
            load();
        });
    }

    function decide(loan, decision) {
        post(`/api/loans/${loan.ID}/${decision}`, {}, () => {
            loadPending();
            load();
        });
    }

    function show(text, type) {
        message = text;
        messageType = type;
        setTimeout(() => (message = ''), 6000);
    }

    const since = (d) => new Date(d).toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
</script>

<svelte:head><title>Equipment · RRC</title></svelte:head>

<div class="page">
    <header class="page-head">
        <div class="title">
            <span class="icon-badge" style="--hue: {AREA.equipment}"><Icon name="box" /></span>
            <div>
                <h1>Equipment</h1>
                <p>What's in the lab and who has it right now.</p>
            </div>
        </div>
        <input class="search" type="search" bind:value={query} placeholder="Search items, labs or people" aria-label="Search" />
    </header>

    {#if message}<div class="message {messageType}" role="status">{message}</div>{/if}

    {#if pendingLoans.length}
        <section class="card approvals">
            <h2 class="label">Waiting for your approval</h2>
            {#each pendingLoans as loan (loan.ID)}
                <div class="row">
                    <span><strong>{loan.borrower_name}</strong> <span class="muted">wants</span> <span class="mono">{loan.quantity_borrowed} ×</span> {loan.item_name} <span class="muted">until</span> <span class="mono">{loan.expected_return_date}</span></span>
                    <span class="buttons">
                        {#if loan.user_id !== user?.id}
                            <button class="btn primary" on:click={() => decide(loan, 'approve')}>Approve</button>
                        {/if}
                        <button class="btn" on:click={() => decide(loan, 'reject')}>Decline</button>
                    </span>
                </div>
            {/each}
        </section>
    {/if}

    {#if loading}
        <p class="note">Loading…</p>
    {:else if items.length === 0}
        <p class="note">Nothing yet. Items appear here as people borrow them.</p>
    {:else if shown.length === 0}
        <p class="note">No items match.</p>
    {:else}
        <ul class="grid items">
            {#each shown as item (item.ID)}
                {@const active = item.out.filter((l) => l.status === 'active')}
                {@const outCount = active.reduce((n, l) => n + l.quantity_borrowed, 0)}
                {@const free = Math.max(item.total_quantity - outCount, 0)}
                <li class="card item">
                    <h3>{item.name}</h3>
                    <p class="meta">
                        <span class="lab muted"><span class="dot" style="--hue: {LAB[item.home_lab] || 'var(--faint)'}"></span>{item.home_lab || 'Lab unknown'}</span>
                        {#if item.total_quantity}
                            <span class="mono" class:free={free > 0} class:none={free === 0}>{free} of {item.total_quantity} free</span>
                        {:else}
                            <span class="mono" class:free={!outCount}>{outCount ? `${outCount} out` : 'In the lab'}</span>
                        {/if}
                    </p>
                    {#if item.tracked || item.lab_only || item.needs_approval}
                        <p class="chips">
                            {#if item.tracked}<span class="chip hue" style="--hue: var(--ctp-lavender)">Tracked</span>{/if}
                            {#if item.lab_only}<span class="chip hue" style="--hue: var(--ctp-yellow)">Lab only</span>{/if}
                            {#if item.needs_approval}<span class="chip warn">Needs approval</span>{/if}
                        </p>
                    {/if}
                    {#if item.notes}<p class="notes muted">{item.notes}</p>{/if}
                    {#if item.issue}
                        <div class="issue">
                            <div>
                                <p class="issue-label">Reported problem</p>
                                <p class="issue-text">{item.issue} <span class="muted">· {item.issue_reported_by}</span></p>
                            </div>
                            {#if user}<button class="btn" on:click={() => confirm(`Mark the problem with ${item.name} as fixed?`) && saveIssue(item, '')}>Fixed</button>{/if}
                        </div>
                    {/if}
                    {#if item.out.length}
                        <div class="holders">
                            {#each item.out as loan (loan.ID)}
                                <div class="row">
                                    <span>
                                        {loan.borrower_name}
                                        {#if loan.quantity_borrowed > 1}<span class="mono">({loan.quantity_borrowed})</span>{/if}
                                        <span class="muted mono small">since {since(loan.CreatedAt)}, due {loan.expected_return_date}</span>
                                        {#if loan.status === 'pending'}<span class="chip warn">Pending</span>{/if}
                                    </span>
                                    {#if loan.status === 'active' && (!user || loan.user_id !== user.id)}
                                        <button class="btn" on:click={() => takeOver(item, loan)}>I have this now</button>
                                    {/if}
                                </div>
                            {/each}
                        </div>
                    {/if}
                    <div class="actions">
                        {#if issueFor === item.ID}
                            <form class="issue-form" on:submit|preventDefault={() => saveIssue(item, issueText)}>
                                <input bind:value={issueText} maxlength="300" placeholder="What's wrong? e.g. USB port loose" required aria-label="Describe the problem" />
                                <span class="buttons">
                                    <button class="btn primary" type="submit">Report</button>
                                    <button class="btn" type="button" on:click={() => (issueFor = null)}>Cancel</button>
                                </span>
                            </form>
                        {:else}
                            <a class="btn" href="/?borrow={encodeURIComponent(item.name)}">Borrow</a>
                            <button class="btn" on:click={() => { issueFor = item.ID; issueText = ''; }}>Report a problem</button>
                        {/if}
                    </div>
                </li>
            {/each}
        </ul>
    {/if}
</div>

<style>
    .search {
        width: min(100%, 320px);
        min-height: 40px;
        box-sizing: border-box;
        padding: 8px 12px;
    }

    .approvals {
        margin-bottom: 14px;
    }

    .row {
        flex-wrap: wrap;
    }

    .buttons {
        display: flex;
        gap: 8px;
    }

    .items {
        list-style: none;
        margin: 0;
        padding: 0;
        grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr));
        align-items: start;
    }

    .item {
        display: flex;
        flex-direction: column;
        gap: 10px;
    }

    .item h3 {
        margin: 0;
        font-size: 1.05rem;
        font-weight: 600;
    }

    .item p {
        margin: 0;
    }

    .meta {
        display: flex;
        justify-content: space-between;
        gap: 8px;
        flex-wrap: wrap;
        font-size: 0.9rem;
    }

    .title {
        display: flex;
        align-items: center;
        gap: 12px;
    }

    .lab {
        display: inline-flex;
        align-items: center;
        gap: 6px;
    }

    .free {
        color: var(--ctp-green);
    }

    .none {
        color: var(--warn);
    }

    .chips {
        display: flex;
        gap: 6px;
        flex-wrap: wrap;
    }

    .notes,
    .small {
        font-size: 0.85rem;
    }

    .issue {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 10px;
        padding: 10px 12px;
        border-left: 3px solid var(--warn);
        border-radius: var(--radius-sm);
        background: color-mix(in srgb, var(--warn) 10%, transparent);
    }

    .issue-label {
        font-size: 0.8rem;
        color: var(--muted);
    }

    .issue-text {
        color: var(--warn);
        font-size: 0.9rem;
    }

    .holders {
        border-top: 1px solid var(--line);
    }

    .holders .row {
        padding: 8px 0;
        font-size: 0.9rem;
    }

    .actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
    }

    .issue-form {
        display: flex;
        gap: 8px;
        flex: 1;
        flex-wrap: wrap;
    }

    .issue-form input {
        flex: 1;
        min-width: 180px;
        min-height: 40px;
        box-sizing: border-box;
        padding: 8px 10px;
    }

    .note {
        color: var(--muted);
        text-align: center;
    }
</style>
