<script>
    // What's in the lab: every item, who has it right now, and any reported
    // problem. People keep it accurate as they go - handing gear on, flagging
    // issues - so nobody has to run a stocktake.
    import { onMount } from 'svelte';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import { LAB } from '$lib/palette.js';
    import IconBorrow from '@tabler/icons-svelte-runes/icons/package-export';
    import IconOut from '@tabler/icons-svelte-runes/icons/arrow-up-right';
    import IconHourglass from '@tabler/icons-svelte-runes/icons/hourglass';
    import IconSearch from '@tabler/icons-svelte-runes/icons/search';
    import IconX from '@tabler/icons-svelte-runes/icons/x';
    import IconCheck from '@tabler/icons-svelte-runes/icons/check';
    import IconAlert from '@tabler/icons-svelte-runes/icons/alert-triangle';
    import IconFlag from '@tabler/icons-svelte-runes/icons/flag';
    import IconBox from '@tabler/icons-svelte-runes/icons/box';
    import IconLock from '@tabler/icons-svelte-runes/icons/lock';
    import IconHome from '@tabler/icons-svelte-runes/icons/home';
    import IconHand from '@tabler/icons-svelte-runes/icons/hand-stop';
    import IconHandoff from '@tabler/icons-svelte-runes/icons/arrows-exchange';

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

{#snippet labDot(lab)}
    <span class="status-dot flex-shrink-0" style="background: {LAB[lab] || 'var(--tblr-tertiary-color)'}"></span>
{/snippet}

{#snippet availability(item)}
    {@const outCount = outOf(item)}
    {@const free = Math.max((item.total_quantity || 0) - outCount, 0)}
    {#if item.total_quantity}
        <span class="badge text-nowrap {free > 0 ? 'bg-success-lt' : 'bg-danger-lt'}">{free} of {item.total_quantity} free</span>
    {:else}
        <span class="badge text-nowrap {outCount ? 'bg-danger-lt' : 'bg-success-lt'}">{outCount ? `${outCount} out` : 'In the lab'}</span>
    {/if}
{/snippet}

{#snippet tags(item)}
    {#if item.tracked || item.lab_only || item.needs_approval}
        <div class="d-flex flex-wrap gap-1 mt-1">
            {#if item.tracked}<span class="badge bg-primary-lt"><IconLock size={12} class="icon me-1" />Tracked</span>{/if}
            {#if item.lab_only}<span class="badge bg-warning-lt"><IconHome size={12} class="icon me-1" />Lab only</span>{/if}
            {#if item.needs_approval}<span class="badge bg-pink-lt"><IconHand size={12} class="icon me-1" />Needs approval</span>{/if}
        </div>
    {/if}
{/snippet}

{#snippet issueBlock(item)}
    {#if item.issue}
        <div class="alert alert-warning py-2 px-3 mb-0 mt-2">
            <div class="d-flex align-items-start gap-2">
                <IconAlert size={18} class="icon alert-icon flex-shrink-0 mt-1" />
                <div class="flex-fill min-w-0">
                    <div class="fw-bold small">Reported problem</div>
                    <div class="small text-break">{item.issue} <span class="text-secondary">· {item.issue_reported_by}</span></div>
                </div>
                {#if user}
                    <button
                        class="btn btn-sm btn-outline-secondary flex-shrink-0"
                        type="button"
                        on:click={() => confirm(`Mark the problem with ${item.name} as fixed?`) && saveIssue(item, '')}
                    >Fixed</button>
                {/if}
            </div>
        </div>
    {/if}
{/snippet}

{#snippet holders(item)}
    {#each item.out as loan (loan.ID)}
        <div class="d-flex flex-wrap align-items-center justify-content-between gap-2 py-1">
            <div class="min-w-0">
                <div class="d-flex flex-wrap align-items-center gap-1">
                    <strong>{loan.borrower_name || 'Someone'}</strong>
                    {#if loan.quantity_borrowed > 1}<span class="tabular text-secondary">({loan.quantity_borrowed})</span>{/if}
                    {#if loan.status === 'pending'}<span class="badge bg-warning-lt">Pending</span>{/if}
                    {#if loan.overdue}<span class="badge bg-danger">Overdue{loan.days_overdue ? ` ${loan.days_overdue}d` : ''}</span>{/if}
                </div>
                <div class="small text-secondary tabular">since {since(loan.CreatedAt)}, due {loan.expected_return_date}</div>
            </div>
            {#if loan.status === 'active' && (!user || loan.user_id !== user.id)}
                <button class="btn btn-sm btn-outline-secondary" type="button" on:click={() => takeOver(item, loan)}>
                    <IconHandoff size={16} class="icon" /> I have this now
                </button>
            {/if}
        </div>
    {/each}
{/snippet}

{#snippet actions(item)}
    {#if issueFor === item.ID}
        <form class="d-flex flex-wrap gap-2" on:submit|preventDefault={() => saveIssue(item, issueText)}>
            <input
                class="form-control flex-fill issue-input"
                bind:value={issueText}
                maxlength="300"
                placeholder="What's wrong? e.g. USB port loose"
                required
                aria-label="Describe the problem"
            />
            <div class="btn-list flex-nowrap">
                <button class="btn btn-primary" type="submit">Report</button>
                <button class="btn btn-outline-secondary" type="button" on:click={() => (issueFor = null)}>Cancel</button>
            </div>
        </form>
    {:else}
        <div class="btn-list flex-nowrap justify-content-md-end">
            <a class="btn btn-primary" href="/?borrow={encodeURIComponent(item.name)}">
                <IconBorrow size={18} class="icon" /> Borrow
            </a>
            <button class="btn btn-outline-secondary" type="button" on:click={() => { issueFor = item.ID; issueText = ''; }}>
                <IconFlag size={18} class="icon" /> Report a problem
            </button>
        </div>
    {/if}
{/snippet}

<div class="page-header d-print-none mt-0 mb-3">
    <div class="row g-2 align-items-center">
        <div class="col">
            <div class="page-pretitle">Inventory</div>
            <h1 class="page-title">What's in the Lab</h1>
            <div class="text-secondary mt-1">Every item, who has it right now, and any reported problem</div>
        </div>
        <div class="col-12 col-sm-auto ms-auto">
            <a class="btn btn-primary w-100" href="/?view=borrow">
                <IconBorrow size={18} class="icon" /> Borrow something
            </a>
        </div>
    </div>
</div>

{#if message}<div class="toast-msg {messageType}" role="status">{message}</div>{/if}

{#if pendingLoans.length}
    <section class="card mb-3">
        <div class="card-status-top bg-warning"></div>
        <div class="card-header">
            <h3 class="card-title d-flex align-items-center gap-2">
                <IconHourglass size={18} class="icon text-warning" /> Waiting for your approval
            </h3>
            <div class="card-actions"><span class="badge bg-warning-lt">{pendingLoans.length}</span></div>
        </div>
        <div class="list-group list-group-flush">
            {#each pendingLoans as loan (loan.ID)}
                <div class="list-group-item">
                    <div class="row g-2 align-items-center">
                        <div class="col-12 col-md">
                            <strong>{loan.borrower_name || 'Someone'}</strong>
                            <span class="text-secondary">wants</span>
                            <span class="tabular">{loan.quantity_borrowed} ×</span>
                            {loan.item_name}
                            <span class="text-secondary">until</span>
                            <span class="tabular">{loan.expected_return_date}</span>
                        </div>
                        <div class="col-12 col-md-auto">
                            <div class="btn-list">
                                {#if loan.user_id !== user?.id}
                                    <button class="btn btn-success" type="button" on:click={() => decide(loan, 'approve')}>Approve</button>
                                {/if}
                                <button class="btn btn-outline-danger" type="button" on:click={() => decide(loan, 'reject')}>Decline</button>
                            </div>
                        </div>
                    </div>
                </div>
            {/each}
        </div>
    </section>
{/if}

<div class="card">
    <div class="card-header flex-column align-items-stretch gap-3">
        <div class="input-icon">
            <span class="input-icon-addon"><IconSearch size={18} class="icon" /></span>
            <input
                type="search"
                class="form-control"
                bind:value={query}
                placeholder="Search items, labs, notes or people"
                aria-label="Search"
            />
            {#if query}
                <button class="input-icon-addon clear-btn btn btn-link p-0" type="button" on:click={() => (query = '')} aria-label="Clear search">
                    <IconX size={18} class="icon" />
                </button>
            {/if}
        </div>
        <div class="d-flex flex-wrap gap-2">
            <div class="filter-scroll">
                <div class="nav nav-segmented" role="group" aria-label="Show">
                    <button class="nav-link" class:active={show_ === 'all'} aria-pressed={show_ === 'all'} type="button" on:click={() => (show_ = 'all')}>
                        All <span class="badge bg-secondary-lt ms-1 tabular">{counts.all}</span>
                    </button>
                    <button class="nav-link" class:active={show_ === 'free'} aria-pressed={show_ === 'free'} type="button" on:click={() => (show_ = 'free')}>
                        <IconCheck size={16} class="icon me-1 text-success" />Available <span class="badge bg-secondary-lt ms-1 tabular">{counts.free}</span>
                    </button>
                    <button class="nav-link" class:active={show_ === 'out'} aria-pressed={show_ === 'out'} type="button" on:click={() => (show_ = 'out')}>
                        <IconOut size={16} class="icon me-1 text-info" />Borrowed <span class="badge bg-secondary-lt ms-1 tabular">{counts.out}</span>
                    </button>
                    <button class="nav-link" class:active={show_ === 'issues'} aria-pressed={show_ === 'issues'} type="button" on:click={() => (show_ = 'issues')}>
                        <IconAlert size={16} class="icon me-1 text-warning" />Problems <span class="badge bg-secondary-lt ms-1 tabular">{counts.issues}</span>
                    </button>
                </div>
            </div>
            {#if labs.length > 1}
                <div class="filter-scroll">
                    <div class="nav nav-segmented" role="group" aria-label="Lab">
                        <button class="nav-link" class:active={labFilter === ''} aria-pressed={labFilter === ''} type="button" on:click={() => (labFilter = '')}>Every lab</button>
                        {#each labs as lab}
                            <button
                                class="nav-link gap-2"
                                class:active={labFilter === lab}
                                aria-pressed={labFilter === lab}
                                type="button"
                                on:click={() => (labFilter = labFilter === lab ? '' : lab)}
                            >
                                {@render labDot(lab)}{lab}
                            </button>
                        {/each}
                    </div>
                </div>
            {/if}
        </div>
    </div>

    {#if loading}
        <div class="card-body text-secondary">Loading…</div>
    {:else if items.length === 0}
        <div class="card-body">
            <div class="empty">
                <div class="empty-icon"><IconBox size={48} class="icon" /></div>
                <p class="empty-title">Nothing yet</p>
                <p class="empty-subtitle text-secondary">Items appear here as people borrow them.</p>
            </div>
        </div>
    {:else if shown.length === 0}
        <div class="card-body">
            <div class="empty">
                <div class="empty-icon"><IconSearch size={48} class="icon" /></div>
                <p class="empty-title">No items match</p>
                <p class="empty-subtitle text-secondary">Try another search or filter.</p>
                <div class="empty-action">
                    <button class="btn btn-outline-primary" type="button" on:click={clearFilters}>Clear filters</button>
                </div>
            </div>
        </div>
    {:else}
        {#if filtered}
            <div class="card-body border-bottom py-2 small text-secondary">
                Showing {shown.length} of {items.length} items ·
                <button class="btn btn-link btn-sm p-0 align-baseline" type="button" on:click={clearFilters}>Clear filters</button>
            </div>
        {/if}

        <!-- Desktop: one table row per item -->
        <div class="table-responsive d-none d-md-block">
            <table class="table table-vcenter card-table">
                <thead>
                    <tr>
                        <th>Item</th>
                        <th>Available</th>
                        <th>Who has it</th>
                        <th class="w-1"><span class="visually-hidden">Actions</span></th>
                    </tr>
                </thead>
                <tbody>
                    {#each shown as item (item.ID)}
                        <tr class="align-top">
                            <td class="item-col">
                                <div class="fw-medium text-break">{item.name}</div>
                                <div class="d-flex align-items-center gap-2 small text-secondary mt-1">
                                    {@render labDot(item.home_lab)}{item.home_lab || 'Lab unknown'}
                                </div>
                                {@render tags(item)}
                                {#if item.notes}<div class="small text-secondary mt-1 text-break">{item.notes}</div>{/if}
                                {@render issueBlock(item)}
                            </td>
                            <td>{@render availability(item)}</td>
                            <td class="holders-col">
                                {#if item.out.length}
                                    {@render holders(item)}
                                {:else}
                                    <span class="text-secondary">-</span>
                                {/if}
                            </td>
                            <td class="text-end">
                                {#if issueFor !== item.ID}{@render actions(item)}{/if}
                            </td>
                        </tr>
                        {#if issueFor === item.ID}
                            <tr>
                                <td colspan="4" class="bg-surface-secondary">{@render actions(item)}</td>
                            </tr>
                        {/if}
                    {/each}
                </tbody>
            </table>
        </div>

        <!-- Phones: one stacked card per item -->
        <div class="list-group list-group-flush d-md-none">
            {#each shown as item (item.ID)}
                <div class="list-group-item">
                    <div class="d-flex align-items-start justify-content-between gap-2">
                        <div class="fw-medium text-break">{item.name}</div>
                        {@render availability(item)}
                    </div>
                    <div class="d-flex align-items-center gap-2 small text-secondary mt-1">
                        {@render labDot(item.home_lab)}{item.home_lab || 'Lab unknown'}
                    </div>
                    {@render tags(item)}
                    {#if item.notes}<div class="small text-secondary mt-1 text-break">{item.notes}</div>{/if}
                    {@render issueBlock(item)}
                    {#if item.out.length}
                        <div class="subheader mt-3 mb-1">Who has it</div>
                        {@render holders(item)}
                    {/if}
                    <div class="mt-3 phone-actions">{@render actions(item)}</div>
                </div>
            {/each}
        </div>
    {/if}
</div>

<style>
    /* Segmented filters scroll sideways on narrow phones instead of the page */
    .filter-scroll {
        max-width: 100%;
        overflow-x: auto;
    }

    .filter-scroll .nav-link {
        white-space: nowrap;
    }

    .clear-btn {
        left: auto;
        right: 0;
        pointer-events: auto;
        color: var(--tblr-secondary-color);
    }

    .item-col {
        min-width: 16rem;
    }

    .holders-col {
        min-width: 14rem;
    }

    .issue-input {
        min-width: 12rem;
    }

    /* On phones the two buttons share the row */
    .phone-actions :global(.btn-list) {
        flex-wrap: wrap !important;
    }

    .phone-actions :global(.btn-list .btn) {
        flex: 1 1 auto;
    }
</style>
