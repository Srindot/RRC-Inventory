<script>
    // What's in the lab: every item, who has it right now, and any reported
    // problem. People keep it accurate as they go - handing gear on, flagging
    // issues - so nobody has to run a stocktake.
    import { onMount } from 'svelte';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import { LAB } from '$lib/palette.js';

    let user = null;
    let items = [];
    let pendingLoans = [];
    let query = '';
    let labFilter = ''; // '' = every lab
    let show_ = 'all'; // all | free | out | issues
    let loading = true;
    let message = '';
    let messageType = '';
    let issueFor = null;
    let issueText = '';

    const activeOf = (i) => i.out.filter((l) => l.status === 'active');
    const outOf = (i) => activeOf(i).reduce((n, l) => n + (l.quantity_borrowed || 0), 0);
    const freeOf = (i) => (i.total_quantity ? Math.max(i.total_quantity - outOf(i), 0) : outOf(i) ? 0 : 1);

    $: labs = [...new Set(items.map((i) => i.home_lab).filter(Boolean))].sort();
    $: counts = {
        all: items.length,
        free: items.filter((i) => freeOf(i) > 0).length,
        out: items.filter((i) => outOf(i) > 0).length,
        issues: items.filter((i) => i.issue).length
    };
    $: shown = items.filter((i) => {
        const q = query.trim().toLowerCase();
        if (labFilter && i.home_lab !== labFilter) return false;
        if (show_ === 'free' && freeOf(i) === 0) return false;
        if (show_ === 'out' && outOf(i) === 0) return false;
        if (show_ === 'issues' && !i.issue) return false;
        return !q || (i.name || '').toLowerCase().includes(q) || (i.home_lab || '').toLowerCase().includes(q) ||
               (i.notes || '').toLowerCase().includes(q) ||
               i.out.some((l) => (l.borrower_name || '').toLowerCase().includes(q));
    });
    $: filtered = query.trim() || labFilter || show_ !== 'all';

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
        const holder = loan.borrower_name || 'the current holder';
        if (can(user, 'manage_inventory')) {
            if (confirm(`Move ${item.name} from ${holder} to your account?`)) {
                post(`/api/loans/${loan.ID}/handover`, { accept: true }, load);
            }
            return;
        }
        // The holder's phone is the proof they handed it over, as for a return.
        const phone = prompt(`Take ${item.name} over from ${holder}?\n\nIt moves to your account and you're responsible for it. Enter ${holder}'s phone number to confirm they gave it to you:`);
        if (phone !== null) post(`/api/loans/${loan.ID}/handover`, { accept: true, phone }, load);
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

    function clearFilters() {
        query = '';
        labFilter = '';
        show_ = 'all';
    }

    function show(text, type) {
        message = text;
        messageType = type;
        setTimeout(() => (message = ''), 6000);
    }

    const since = (d) => new Date(d).toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
</script>

<svelte:head><title>What's in the Lab · RRC</title></svelte:head>

<div class="container">
    <div class="page-header">
        <div>
            <h1>🧰 What's in the Lab</h1>
            <p class="subtitle">Every item, who has it right now, and any reported problem</p>
        </div>
        <a class="ghost-btn" href="/?view=borrow">📦 Borrow something</a>
    </div>

    {#if message}<div class="message {messageType}" role="status">{message}</div>{/if}

    {#if pendingLoans.length}
        <section class="panel approvals">
            <h3 class="section-title">⏳ Waiting for your approval <span class="count">{pendingLoans.length}</span></h3>
            {#each pendingLoans as loan (loan.ID)}
                <div class="list-row">
                    <span><strong>{loan.borrower_name || 'Someone'}</strong> <span class="muted">wants</span> <span class="mono">{loan.quantity_borrowed} ×</span> {loan.item_name} <span class="muted">until</span> <span class="mono">{loan.expected_return_date}</span></span>
                    <span class="buttons">
                        {#if loan.user_id !== user?.id}
                            <button class="submit-btn small" on:click={() => decide(loan, 'approve')}>Approve</button>
                        {/if}
                        <button class="ghost-btn" on:click={() => decide(loan, 'reject')}>Decline</button>
                    </span>
                </div>
            {/each}
        </section>
    {/if}

    <section class="panel toolbar">
        <div class="search-input-group">
            <input type="search" bind:value={query} placeholder="🔍 Search items, labs, notes or people" aria-label="Search" />
            {#if query}<button class="clear-search-btn" on:click={() => (query = '')} aria-label="Clear search">×</button>{/if}
        </div>
        <div class="filter-rows">
            <div class="chip-row" role="group" aria-label="Show">
                <button class="chip" class:selected={show_ === 'all'} on:click={() => (show_ = 'all')}>All <span class="mono">{counts.all}</span></button>
                <button class="chip" class:selected={show_ === 'free'} on:click={() => (show_ = 'free')}>✅ Available <span class="mono">{counts.free}</span></button>
                <button class="chip" class:selected={show_ === 'out'} on:click={() => (show_ = 'out')}>📤 Borrowed <span class="mono">{counts.out}</span></button>
                <button class="chip" class:selected={show_ === 'issues'} on:click={() => (show_ = 'issues')}>⚠️ Problems <span class="mono">{counts.issues}</span></button>
            </div>
            {#if labs.length > 1}
                <div class="chip-row" role="group" aria-label="Lab">
                    <button class="chip" class:selected={labFilter === ''} on:click={() => (labFilter = '')}>Every lab</button>
                    {#each labs as lab}
                        <button class="chip lab-chip" class:selected={labFilter === lab} style="--hue: {LAB[lab] || 'var(--faint)'}" on:click={() => (labFilter = labFilter === lab ? '' : lab)}>
                            <span class="dot"></span>{lab}
                        </button>
                    {/each}
                </div>
            {/if}
        </div>
    </section>

    {#if loading}
        <p class="note">Loading…</p>
    {:else if items.length === 0}
        <p class="note">Nothing yet. Items appear here as people borrow them.</p>
    {:else if shown.length === 0}
        <p class="note">No items match. <button class="link-btn" on:click={clearFilters}>Clear filters</button></p>
    {:else}
        {#if filtered}
            <p class="result-count">Showing {shown.length} of {items.length} items · <button class="link-btn" on:click={clearFilters}>Clear filters</button></p>
        {/if}
        <ul class="items-grid">
            {#each shown as item (item.ID)}
                {@const outCount = outOf(item)}
                {@const free = Math.max((item.total_quantity || 0) - outCount, 0)}
                <li class="item-card" class:has-issue={item.issue}>
                    <div class="item-head">
                        <h3>{item.name}</h3>
                        {#if item.total_quantity}
                            <span class="status-badge" class:ok={free > 0} class:none={free === 0}>{free} of {item.total_quantity} free</span>
                        {:else}
                            <span class="status-badge" class:ok={!outCount} class:none={outCount}>{outCount ? `${outCount} out` : 'In the lab'}</span>
                        {/if}
                    </div>
                    <p class="lab"><span class="dot" style="--hue: {LAB[item.home_lab] || 'var(--faint)'}"></span>{item.home_lab || 'Lab unknown'}</p>
                    {#if item.tracked || item.lab_only || item.needs_approval}
                        <p class="tags">
                            {#if item.tracked}<span class="tag" style="--hue: var(--rp-iris)">🔒 Tracked</span>{/if}
                            {#if item.lab_only}<span class="tag" style="--hue: var(--rp-gold)">🏠 Lab only</span>{/if}
                            {#if item.needs_approval}<span class="tag" style="--hue: var(--rp-rose)">✋ Needs approval</span>{/if}
                        </p>
                    {/if}
                    {#if item.notes}<p class="notes">{item.notes}</p>{/if}
                    {#if item.issue}
                        <div class="issue">
                            <div>
                                <p class="issue-label">⚠️ Reported problem</p>
                                <p class="issue-text">{item.issue} <span class="muted">· {item.issue_reported_by}</span></p>
                            </div>
                            {#if user}<button class="ghost-btn" on:click={() => confirm(`Mark the problem with ${item.name} as fixed?`) && saveIssue(item, '')}>Fixed</button>{/if}
                        </div>
                    {/if}
                    {#if item.out.length}
                        <div class="holders">
                            <p class="holders-label">Who has it</p>
                            {#each item.out as loan (loan.ID)}
                                <div class="holder">
                                    <span class="holder-text">
                                        <strong>{loan.borrower_name || 'Someone'}</strong>
                                        {#if loan.quantity_borrowed > 1}<span class="mono">({loan.quantity_borrowed})</span>{/if}
                                        {#if loan.status === 'pending'}<span class="status-badge warn">Pending</span>{/if}
                                        {#if loan.overdue}<span class="status-badge overdue">Overdue{loan.days_overdue ? ` ${loan.days_overdue}d` : ''}</span>{/if}
                                        <span class="when mono">since {since(loan.CreatedAt)}, due {loan.expected_return_date}</span>
                                    </span>
                                    {#if loan.status === 'active' && (!user || loan.user_id !== user.id)}
                                        <button class="ghost-btn" on:click={() => takeOver(item, loan)}>🤝 I have this now</button>
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
                                    <button class="submit-btn small" type="submit">Report</button>
                                    <button class="ghost-btn" type="button" on:click={() => (issueFor = null)}>Cancel</button>
                                </span>
                            </form>
                        {:else}
                            <a class="submit-btn small" href="/?borrow={encodeURIComponent(item.name)}">📦 Borrow</a>
                            <button class="ghost-btn" on:click={() => { issueFor = item.ID; issueText = ''; }}>⚠️ Report a problem</button>
                        {/if}
                    </div>
                </li>
            {/each}
        </ul>
    {/if}
</div>

<style>
    .container {
        max-width: 1200px;
        margin: 0 auto;
        padding: clamp(10px, 2vw, 20px) clamp(16px, 2vw, 20px) 40px;
        box-sizing: border-box;
    }

    .page-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
        border-bottom: 1px solid var(--ctp-surface0);
        padding-bottom: 12px;
        margin-bottom: 18px;
    }

    h1 {
        margin: 0;
        font-size: clamp(1.3rem, 3vw, 1.8rem);
        font-weight: 600;
        color: var(--rp-foam);
    }

    .subtitle {
        margin: 2px 0 0;
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
    }

    .panel {
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--rp-text) 4%, transparent), transparent 60%),
            var(--card);
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        padding: clamp(14px, 2.5vw, 20px);
        margin-bottom: 16px;
        box-shadow: var(--shadow-sm);
    }

    .section-title {
        margin: 0 0 10px;
        font-size: 1.05rem;
        color: var(--ctp-text);
        border-left: 4px solid var(--rp-gold);
        padding-left: 12px;
    }

    .count {
        display: inline-block;
        margin-left: 6px;
        padding: 2px 8px;
        border-radius: 12px;
        background: var(--rp-love);
        color: var(--accent-ink);
        font-size: 0.75rem;
        vertical-align: 2px;
    }

    .list-row {
        display: flex;
        justify-content: space-between;
        align-items: center;
        flex-wrap: wrap;
        gap: 10px;
        padding: 10px 0;
        border-top: 1px solid var(--ctp-surface0);
    }

    .buttons {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
    }

    /* --- search and filters ------------------------------------------- */

    .toolbar {
        display: flex;
        flex-direction: column;
        gap: 12px;
    }

    .search-input-group {
        position: relative;
    }

    .search-input-group input,
    .issue-form input {
        width: 100%;
        min-height: 44px;
        padding: 10px 14px;
        border: 2px solid var(--ctp-surface0);
        border-radius: 8px;
        background: var(--bg);
        color: var(--ctp-text);
        font: inherit;
        box-sizing: border-box;
        transition: border-color var(--fast) var(--ease), box-shadow var(--fast) var(--ease);
    }

    .search-input-group input {
        padding-right: 50px;
    }

    .search-input-group input:focus,
    .issue-form input:focus {
        border-color: var(--rp-rose);
        outline: none;
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--rp-rose) 25%, transparent);
    }

    .clear-search-btn {
        position: absolute;
        right: 8px;
        top: 50%;
        transform: translateY(-50%);
        width: 30px;
        height: 30px;
        border: none;
        border-radius: 50%;
        background: var(--rp-love);
        color: var(--accent-ink);
        font-size: 16px;
        font-weight: bold;
        cursor: pointer;
    }

    .filter-rows {
        display: flex;
        flex-direction: column;
        gap: 8px;
    }

    .chip-row {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
    }

    .chip {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        min-height: 40px;
        padding: 6px 14px;
        border: 1px solid var(--ctp-surface1);
        border-radius: 999px;
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        font: inherit;
        font-size: 0.9rem;
        cursor: pointer;
        transition: background var(--fast) var(--ease);
    }

    .chip:hover {
        background: var(--ctp-surface1);
    }

    .chip .mono {
        opacity: 0.75;
    }

    .chip.selected {
        background: var(--rp-rose);
        border-color: var(--rp-rose);
        color: var(--accent-ink);
        font-weight: 600;
    }

    .chip.selected .dot {
        background: var(--accent-ink);
    }

    .dot {
        display: inline-block;
        flex: none;
        width: 8px;
        height: 8px;
        border-radius: 50%;
        background: var(--hue, var(--faint));
    }

    .result-count {
        margin: 0 0 4px;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
    }

    .link-btn {
        background: none;
        border: none;
        padding: 0;
        color: var(--link);
        font: inherit;
        text-decoration: underline;
        cursor: pointer;
    }

    /* --- item cards ---------------------------------------------------- */

    .items-grid {
        list-style: none;
        margin: 0;
        padding: 0;
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr));
        gap: clamp(12px, 2vw, 18px);
        align-items: start;
        margin-top: 12px;
    }

    .item-card {
        display: flex;
        flex-direction: column;
        gap: 10px;
        min-width: 0;
        padding: clamp(15px, 3vw, 20px);
        border: 1px solid var(--ctp-surface0);
        border-radius: 12px;
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--rp-text) 4%, transparent), transparent 60%),
            var(--card);
        box-shadow: var(--shadow-sm);
        transition: transform var(--normal) var(--ease), border-color var(--normal) var(--ease), box-shadow var(--normal) var(--ease);
    }

    .item-card:hover {
        border-color: var(--rp-rose);
        transform: translateY(-2px);
        box-shadow: var(--shadow);
    }

    .item-card.has-issue {
        border-left: 3px solid var(--warn);
    }

    .item-card p {
        margin: 0;
    }

    .item-head {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        gap: 10px;
    }

    .item-head h3 {
        margin: 0;
        font-size: 1.08rem;
        font-weight: 600;
        color: var(--ctp-text);
        overflow-wrap: anywhere;
    }

    .status-badge {
        display: inline-block;
        flex: none;
        padding: 3px 10px;
        border-radius: 20px;
        font-size: 0.78rem;
        font-weight: 700;
        white-space: nowrap;
        background: var(--ctp-surface0);
        color: var(--ctp-subtext0);
    }

    .status-badge.ok {
        background: var(--rp-foam);
        color: var(--rp-base);
    }

    .status-badge.none,
    .status-badge.warn {
        background: var(--rp-gold);
        color: var(--rp-base);
    }

    .status-badge.overdue {
        background: var(--rp-love);
        color: var(--accent-ink);
    }

    :root[data-theme='light'] .status-badge.ok {
        background: color-mix(in srgb, var(--rp-foam) 22%, var(--card));
        color: var(--rp-pine);
    }

    :root[data-theme='light'] .status-badge.none,
    :root[data-theme='light'] .status-badge.warn {
        background: color-mix(in srgb, var(--rp-gold) 25%, var(--card));
        color: color-mix(in srgb, var(--rp-gold) 60%, var(--rp-text));
    }

    .lab {
        display: inline-flex;
        align-items: center;
        gap: 8px;
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
    }

    .tags {
        display: flex;
        gap: 6px;
        flex-wrap: wrap;
    }

    .tag {
        padding: 3px 10px;
        border-radius: 999px;
        font-size: 0.78rem;
        font-weight: 600;
        border: 1px solid color-mix(in srgb, var(--hue) 45%, transparent);
        background: color-mix(in srgb, var(--hue) 12%, transparent);
        color: var(--hue);
    }

    :root[data-theme='light'] .tag {
        color: color-mix(in srgb, var(--hue) 75%, var(--rp-text));
    }

    .notes {
        font-size: 0.88rem;
        color: var(--ctp-subtext0);
        line-height: 1.4;
    }

    .issue {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 10px;
        padding: 10px 12px;
        border-radius: 8px;
        border: 1px solid color-mix(in srgb, var(--warn) 40%, transparent);
        background: color-mix(in srgb, var(--warn) 12%, transparent);
    }

    .issue-label {
        font-size: 0.78rem;
        font-weight: 600;
        color: var(--ctp-subtext0);
    }

    .issue-text {
        color: var(--warn);
        font-size: 0.9rem;
    }

    :root[data-theme='light'] .issue-text {
        color: color-mix(in srgb, var(--warn) 60%, var(--rp-text));
    }

    .holders {
        border-top: 1px solid var(--ctp-surface0);
        padding-top: 8px;
    }

    .holders-label {
        font-size: 0.78rem;
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.04em;
        color: var(--ctp-overlay1);
    }

    .holder {
        display: flex;
        justify-content: space-between;
        align-items: center;
        flex-wrap: wrap;
        gap: 8px;
        padding: 8px 0;
        font-size: 0.9rem;
    }

    .holder + .holder {
        border-top: 1px dashed var(--ctp-surface0);
    }

    .holder-text {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 4px 8px;
        min-width: 0;
    }

    .when {
        width: 100%;
        font-size: 0.8rem;
        color: var(--ctp-subtext0);
    }

    .actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
        margin-top: 2px;
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
    }

    /* --- buttons -------------------------------------------------------- */

    .submit-btn,
    .ghost-btn {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        gap: 6px;
        min-height: 40px;
        padding: 8px 16px;
        border-radius: 8px;
        font: inherit;
        font-size: 0.92rem;
        font-weight: 600;
        text-decoration: none;
        cursor: pointer;
        box-sizing: border-box;
        transition: transform var(--fast) var(--ease), box-shadow var(--fast) var(--ease), background var(--fast) var(--ease);
    }

    .submit-btn {
        border: none;
        background: linear-gradient(135deg, var(--rp-rose), var(--rp-love));
        color: var(--accent-ink);
    }

    .submit-btn:hover {
        transform: translateY(-1px);
        box-shadow: 0 6px 18px color-mix(in srgb, var(--rp-rose) 30%, transparent);
    }

    .ghost-btn {
        border: 1px solid var(--ctp-surface1);
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        font-weight: 500;
    }

    .ghost-btn:hover {
        background: var(--ctp-surface1);
        transform: translateY(-1px);
    }

    .muted {
        color: var(--ctp-subtext0);
    }

    .mono {
        font-family: var(--mono);
        font-variant-numeric: tabular-nums;
    }

    .note {
        color: var(--ctp-subtext0);
        text-align: center;
        padding: 30px 0;
    }

    /* Toast in the old message colours */
    .message.success {
        background: color-mix(in srgb, var(--rp-foam) 15%, var(--card));
    }

    .message.error {
        background: color-mix(in srgb, var(--rp-love) 15%, var(--card));
    }

    @media (max-width: 520px) {
        .page-header .ghost-btn {
            width: 100%;
        }
    }
</style>
