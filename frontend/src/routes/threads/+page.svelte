<script>
    // Research threads: a prof's project, its members, and the robots a lab
    // admin has signed off for it. Members book those robots for the thread.
    // The server decides who may change what; the page hides what it'd refuse.
    import { onMount } from 'svelte';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import { hueFor } from '$lib/palette.js';
    import IconRobot from '@tabler/icons-svelte-runes/icons/robot';
    import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
    import IconChevronDown from '@tabler/icons-svelte-runes/icons/chevron-down';
    import IconHourglass from '@tabler/icons-svelte-runes/icons/hourglass';
    import IconCheck from '@tabler/icons-svelte-runes/icons/check';
    import IconX from '@tabler/icons-svelte-runes/icons/x';
    import IconSearch from '@tabler/icons-svelte-runes/icons/search';
    import IconFlask from '@tabler/icons-svelte-runes/icons/flask';
    import IconDeviceFloppy from '@tabler/icons-svelte-runes/icons/device-floppy';
    import IconPencil from '@tabler/icons-svelte-runes/icons/pencil';
    import IconUsers from '@tabler/icons-svelte-runes/icons/users';
    import IconUserPlus from '@tabler/icons-svelte-runes/icons/user-plus';
    import IconCalendar from '@tabler/icons-svelte-runes/icons/calendar-event';
    import IconTrash from '@tabler/icons-svelte-runes/icons/trash';

    let user = null;
    let threads = [];
    let people = [];
    let robots = [];
    let loading = true;
    let message = '';
    let messageType = '';
    let busy = false; // one change at a time, so a double tap can't create two threads

    let newThread = { name: '', description: '' };
    let editing = null; // thread ID being edited
    let edit = { name: '', description: '' };
    let hours = {}; // `${threadID}:${resourceID}` -> hours/week draft
    let addPick = {}; // thread ID -> user id to add
    let robotPick = {}; // thread ID -> resource id to request
    let query = '';

    $: labAdmin = can(user, 'approve_mech_bookings');
    $: mineList = threads.filter((t) => t.mine || t.can_manage);
    $: others = threads.filter((t) => !t.mine && !t.can_manage);
    $: filterQ = query.trim().toLowerCase();
    $: matches = (t) => !filterQ || [t.name, t.description, t.lead_name, ...t.members.map((m) => m.name), ...t.access.map((a) => a.resource_name)]
        .some((x) => (x || '').toLowerCase().includes(filterQ));
    $: mineShown = mineList.filter(matches);
    $: othersShown = others.filter(matches);
    $: waiting = threads.flatMap((t) => t.access.filter((a) => a.status === 'pending').map((a) => ({ t, a })));

    onMount(async () => {
        user = getUser();
        user = await refreshUser();
        if (!user) {
            location.href = loginUrl();
            return;
        }
        load();
        fetch('/api/resources?lab=mech').then((r) => (r.ok ? r.json() : [])).then((r) => (robots = r)).catch(() => {});
        // The people list is only served to those who can add members (see
        // GET /api/admin/people); asking without the permission just 403s.
        if (['create_threads', 'manage_users', 'manage_bookings', 'manage_inventory'].some((p) => can(user, p))) {
            api('/api/admin/people').then((r) => (r.ok ? r.json() : [])).then((p) => (people = p || [])).catch(() => {});
        }
    });

    async function load() {
        try {
            const response = await api('/api/threads');
            if (response.status === 401) {
                location.href = loginUrl();
                return;
            }
            if (!response.ok) throw new Error();
            threads = await response.json();
            hours = {};
            for (const t of threads) for (const a of t.access) hours[key(t, a)] = a.hours_per_week || '';
        } catch {
            show('Could not load research threads', 'error');
        } finally {
            loading = false;
        }
    }

    // send runs one change and reloads on success
    async function send(method, path, body) {
        if (busy) return false;
        busy = true;
        try {
            const response = await api(path, {
                method,
                headers: { 'Content-Type': 'application/json' },
                body: body ? JSON.stringify(body) : undefined
            });
            if (response.status === 401) {
                location.href = loginUrl();
                return false;
            }
            const result = await response.json().catch(() => ({}));
            show(result.message || result.error || `HTTP ${response.status}`, response.ok ? 'success' : 'error');
            if (response.ok) load();
            return response.ok;
        } catch {
            show('Network error. Please try again.', 'error');
            return false;
        } finally {
            busy = false;
        }
    }

    const key = (t, a) => `${t.ID}:${a.resource_id}`;
    const hoursOf = (t, a) => Number(hours[key(t, a)]) || 0;

    async function create() {
        if (await send('POST', '/api/threads', newThread)) newThread = { name: '', description: '' };
    }

    function startEdit(t) {
        editing = t.ID;
        edit = { name: t.name, description: t.description };
    }

    async function saveEdit(t) {
        if (await send('PUT', `/api/threads/${t.ID}`, edit)) editing = null;
    }

    function setActive(t, active) {
        send('PUT', `/api/threads/${t.ID}`, { name: t.name, description: t.description, active });
    }

    function removeMember(t, m, memberIds) {
        if (confirm(`Remove ${m.name} from ${t.name}?`)) setMembers(t, memberIds.filter((id) => id !== m.id));
    }

    function setMembers(t, ids) {
        send('PUT', `/api/threads/${t.ID}/members`, { user_ids: ids });
    }

    function decide(t, a, decision) {
        send('POST', `/api/threads/${t.ID}/resources/${a.resource_id}`, { decision, hours_per_week: hoursOf(t, a) });
    }

    function removeAccess(t, a) {
        if (confirm(`Remove ${t.name}'s access to ${a.resource_name}?`)) send('DELETE', `/api/threads/${t.ID}/resources/${a.resource_id}`);
    }

    function usage(a) {
        const used = (a.used_this_week || 0).toFixed(1);
        return a.hours_per_week ? `${used} of ${a.hours_per_week} h this week` : `${used} h this week, no limit`;
    }

    const usedPct = (a) => Math.min(100, ((a.used_this_week || 0) / a.hours_per_week) * 100);

    function show(text, type) {
        message = text;
        messageType = type;
        setTimeout(() => (message = ''), 6000);
    }
</script>


<svelte:head><title>Research Threads · RRC</title></svelte:head>

<div class="page-header d-print-none mb-3">
    <div class="row g-3 align-items-center">
        <div class="col-12 col-md">
            <div class="page-pretitle">Projects</div>
            <h2 class="page-title">Research threads</h2>
            <div class="text-secondary mt-1">A thread is a project. Its members book the robots a lab admin has signed off for it.</div>
        </div>
        <div class="col-12 col-md-auto">
            <a class="btn btn-primary w-100" href="/mechlab"><IconRobot size={18} class="icon" />Book a robot</a>
        </div>
    </div>
</div>

{#if message}<div class="toast-msg {messageType}" role="status">{message}</div>{/if}

<div class="d-flex flex-column gap-3">
    {#if can(user, 'create_threads')}
        <details class="card fold" open={!loading && mineList.length === 0}>
            <summary class="card-header">
                <h3 class="card-title d-flex align-items-center gap-2">
                    <span class="avatar avatar-sm bg-primary-lt"><IconPlus size={18} class="icon" /></span>
                    Start a new thread
                </h3>
                <span class="card-actions text-secondary"><IconChevronDown size={18} class="icon chevron" /></span>
            </summary>
            <form class="card-body" on:submit|preventDefault={create}>
                <div class="row g-3 align-items-end">
                    <div class="col-12 col-md-4">
                        <label class="form-label" for="nt-name">Name</label>
                        <input id="nt-name" class="form-control" bind:value={newThread.name} maxlength="100" required placeholder="e.g. Grasping" />
                    </div>
                    <div class="col-12 col-md">
                        <label class="form-label" for="nt-desc">Description <span class="text-secondary fw-normal">(optional)</span></label>
                        <input id="nt-desc" class="form-control" bind:value={newThread.description} maxlength="1000" placeholder="What the project is about" />
                    </div>
                    <div class="col-12 col-md-auto">
                        <button class="btn btn-primary w-100" type="submit" disabled={busy}>{busy ? 'Creating...' : 'Create thread'}</button>
                    </div>
                </div>
                <div class="form-hint mt-3">You lead the threads you create: add members, then request robot access for a lab admin to sign off.</div>
            </form>
        </details>
    {/if}

    {#if labAdmin && waiting.length}
        <div class="card">
            <div class="card-header">
                <h3 class="card-title d-flex align-items-center gap-2">
                    <IconHourglass size={18} class="icon text-warning" />
                    Waiting for your sign-off
                    <span class="badge bg-warning-lt">{waiting.length}</span>
                </h3>
            </div>
            <div class="table-responsive">
                <table class="table table-vcenter card-table">
                    <thead>
                        <tr>
                            <th>Thread</th>
                            <th>Robot</th>
                            <th>Hours per week</th>
                            <th class="w-1"><span class="visually-hidden">Actions</span></th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each waiting as { t, a } (key(t, a))}
                            <tr>
                                <td>
                                    <div class="d-flex align-items-center text-nowrap">
                                        <span class="dot" style="--hue: {hueFor(t.ID)}"></span><strong>{t.name}</strong>
                                    </div>
                                </td>
                                <td class="text-nowrap">{a.resource_name}</td>
                                <td>
                                    <div class="input-group hours-group">
                                        <input class="form-control tabular" type="number" min="0" max="168" bind:value={hours[key(t, a)]} placeholder="no limit" aria-label="Hours per week for {t.name} on {a.resource_name}" />
                                        <span class="input-group-text">h/week</span>
                                    </div>
                                </td>
                                <td>
                                    <div class="btn-list flex-nowrap">
                                        <button class="btn btn-success" type="button" disabled={busy} on:click={() => decide(t, a, 'approve')}>
                                            <IconCheck size={18} class="icon" />Approve
                                        </button>
                                        <button class="btn btn-outline-danger" type="button" disabled={busy} on:click={() => decide(t, a, 'decline')}>
                                            <IconX size={18} class="icon" />Decline
                                        </button>
                                    </div>
                                </td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            </div>
        </div>
    {/if}

    <div class="row g-2 align-items-center">
        <div class="col-12 col-sm">
            <h3 class="mb-0 d-flex align-items-center gap-2">
                {labAdmin ? 'All threads' : 'My threads'}
                {#if !loading}<span class="badge bg-secondary-lt">{mineList.length}</span>{/if}
            </h3>
        </div>
        {#if threads.length > 3}
            <div class="col-12 col-sm-auto">
                <div class="input-icon search">
                    <span class="input-icon-addon"><IconSearch size={18} class="icon" /></span>
                    <input class="form-control" type="search" bind:value={query} placeholder="Find a thread, person or robot" aria-label="Find a thread" />
                    {#if query}
                        <button class="btn btn-ghost-secondary btn-icon clear-search" type="button" on:click={() => (query = '')} aria-label="Clear search">
                            <IconX size={16} class="icon" />
                        </button>
                    {/if}
                </div>
            </div>
        {/if}
    </div>

    {#if loading}
        <div class="card">
            <div class="card-body d-flex align-items-center justify-content-center gap-2 text-secondary py-5">
                <span class="spinner-border spinner-border-sm" aria-hidden="true"></span>Loading...
            </div>
        </div>
    {:else if mineList.length === 0}
        <div class="card">
            <div class="card-body">
                <div class="empty">
                    <div class="empty-icon"><IconFlask size={48} class="icon" /></div>
                    <p class="empty-title">You're not in a research thread yet</p>
                    <p class="empty-subtitle text-secondary">Ask your prof to add you{others.length ? ' - here is who leads what.' : '.'}</p>
                </div>
            </div>
        </div>
    {:else if mineShown.length === 0}
        <p class="text-secondary mb-0">
            No threads match "{query}".
            <button class="btn btn-link p-0 align-baseline" type="button" on:click={() => (query = '')}>Clear</button>
        </p>
    {/if}

    {#if mineShown.length}
        <div class="row row-cards g-3 align-items-start">
            {#each mineShown as t (t.ID)}
                {@const memberIds = t.members.map((m) => m.id)}
                {@const requestable = robots.filter((r) => !t.access.some((a) => a.resource_id === r.id))}
                <div class="col-12 col-lg-6 col-xxl-4">
                    <div class="card" class:inactive={!t.active} style="--hue: {hueFor(t.ID)}">
                        <div class="card-status-start hue-bg"></div>
                        {#if editing === t.ID}
                            <form class="card-body border-bottom" on:submit|preventDefault={() => saveEdit(t)}>
                                <div class="mb-3">
                                    <label class="form-label" for="ed-name-{t.ID}">Name</label>
                                    <input id="ed-name-{t.ID}" class="form-control" bind:value={edit.name} maxlength="100" required />
                                </div>
                                <div class="mb-3">
                                    <label class="form-label" for="ed-desc-{t.ID}">Description</label>
                                    <input id="ed-desc-{t.ID}" class="form-control" bind:value={edit.description} maxlength="1000" placeholder="Description" />
                                </div>
                                <div class="btn-list">
                                    <button class="btn btn-primary" type="submit" disabled={busy}><IconDeviceFloppy size={18} class="icon" />Save</button>
                                    <button class="btn btn-outline-secondary" type="button" on:click={() => (editing = null)}>Cancel</button>
                                </div>
                            </form>
                        {:else}
                            <div class="card-header flex-wrap gap-2">
                                <div class="d-flex align-items-center flex-wrap gap-2 me-auto min-w-0">
                                    <span class="dot me-0" style="--hue: {hueFor(t.ID)}"></span>
                                    <h3 class="card-title text-break">{t.name}</h3>
                                    {#if !t.active}<span class="badge bg-secondary-lt">Inactive</span>{/if}
                                    {#if t.mine && !t.can_manage}<span class="badge bg-primary-lt">Member</span>{/if}
                                </div>
                                {#if labAdmin || t.can_manage}
                                    <div class="d-flex align-items-center gap-3">
                                        {#if labAdmin}
                                            <label class="form-check form-switch mb-0">
                                                <input class="form-check-input" type="checkbox" checked={t.active} disabled={busy} on:change={(e) => setActive(t, e.currentTarget.checked)} />
                                                <span class="form-check-label">Active</span>
                                            </label>
                                        {/if}
                                        {#if t.can_manage}
                                            <button class="btn btn-outline-secondary" type="button" on:click={() => startEdit(t)}>
                                                <IconPencil size={18} class="icon" />Edit
                                            </button>
                                        {/if}
                                    </div>
                                {/if}
                            </div>
                        {/if}
                        <div class="card-body pb-2">
                            {#if t.description && editing !== t.ID}<p class="mb-2">{t.description}</p>{/if}
                            <div class="text-secondary">Led by <strong class="text-reset">{t.lead_name}</strong></div>
                        </div>

                        <div class="card-body pt-2 pb-0">
                            <div class="subheader d-flex align-items-center gap-1 mb-2">
                                <IconUsers size={16} class="icon" />Members <span class="tabular">{t.members.length}</span>
                            </div>
                        </div>
                        <div class="list-group list-group-flush">
                            {#each t.members as m (m.id)}
                                <div class="list-group-item py-2">
                                    <div class="d-flex align-items-center gap-2">
                                        <span class="avatar avatar-xs {m.id === t.lead_id ? 'bg-primary-lt' : ''}">{(m.name || '?').trim().charAt(0).toUpperCase()}</span>
                                        <span class="flex-fill text-truncate">{m.name}</span>
                                        {#if m.id === t.lead_id}<span class="badge bg-primary-lt">Lead</span>{/if}
                                        {#if t.can_manage && m.id !== t.lead_id}
                                            <button class="btn btn-ghost-danger btn-icon" type="button" aria-label="Remove {m.name}" title="Remove {m.name}" disabled={busy} on:click={() => removeMember(t, m, memberIds)}>
                                                <IconX size={18} class="icon" />
                                            </button>
                                        {/if}
                                    </div>
                                </div>
                            {/each}
                        </div>
                        {#if t.can_manage && people.length}
                            <div class="card-body py-2">
                                <div class="input-group">
                                    <select class="form-select" bind:value={addPick[t.ID]} aria-label="Add member to {t.name}">
                                        <option value={undefined}>Add member...</option>
                                        {#each people.filter((p) => !memberIds.includes(p.id)) as p (p.id)}
                                            <option value={p.id}>{p.name}</option>
                                        {/each}
                                    </select>
                                    <button class="btn" type="button" disabled={busy || !addPick[t.ID]} on:click={() => { setMembers(t, [...memberIds, addPick[t.ID]]); addPick[t.ID] = undefined; }}>
                                        <IconUserPlus size={18} class="icon" />Add
                                    </button>
                                </div>
                            </div>
                        {/if}

                        <div class="card-body pt-3 pb-0">
                            <div class="subheader d-flex align-items-center gap-1 mb-2"><IconRobot size={16} class="icon" />Robots</div>
                            {#if t.access.length === 0}
                                <p class="text-secondary small mb-3">No robot access yet.{#if t.can_manage && requestable.length}{' Request one below.'}{/if}</p>
                            {/if}
                        </div>
                        {#if t.access.length}
                            <div class="list-group list-group-flush">
                                {#each t.access as a (a.id)}
                                    <div class="list-group-item">
                                        <div class="d-flex align-items-center justify-content-between gap-2">
                                            <span class="d-flex align-items-center min-w-0">
                                                <span class="dot" style="--hue: {hueFor(a.resource_id)}"></span><strong class="text-truncate">{a.resource_name}</strong>
                                            </span>
                                            {#if a.status === 'pending'}
                                                <span class="badge bg-warning-lt">Waiting for sign-off</span>
                                            {:else}
                                                <span class="badge bg-success-lt">Approved</span>
                                            {/if}
                                        </div>
                                        {#if a.status !== 'pending'}
                                            {#if a.hours_per_week}
                                                <div class="progress progress-sm mt-2" role="progressbar" aria-label="Hours used this week" aria-valuenow={Math.round(usedPct(a))} aria-valuemin="0" aria-valuemax="100">
                                                    <div class="progress-bar {usedPct(a) >= 100 ? 'bg-warning' : 'bg-primary'}" style="width: {usedPct(a)}%"></div>
                                                </div>
                                            {/if}
                                            <div class="small text-secondary tabular mt-1">{usage(a)}</div>
                                        {/if}
                                        {#if a.status === 'approved' || t.can_manage}
                                            <div class="d-flex flex-wrap align-items-center gap-2 mt-2">
                                                {#if labAdmin && a.status === 'approved'}
                                                    <div class="input-group hours-group">
                                                        <input class="form-control tabular" type="number" min="0" max="168" bind:value={hours[key(t, a)]} placeholder="no limit" aria-label="Hours per week for {a.resource_name}" />
                                                        <span class="input-group-text">h/week</span>
                                                        <button class="btn" type="button" disabled={busy} on:click={() => decide(t, a, 'approve')}>Save</button>
                                                    </div>
                                                {/if}
                                                <div class="btn-list ms-auto">
                                                    {#if a.status === 'approved' && t.active}<a class="btn btn-outline-primary" href="/mechlab"><IconCalendar size={18} class="icon" />Book</a>{/if}
                                                    {#if t.can_manage}
                                                        <button class="btn btn-outline-danger" type="button" disabled={busy} on:click={() => removeAccess(t, a)}>
                                                            <IconTrash size={18} class="icon" />Remove
                                                        </button>
                                                    {/if}
                                                </div>
                                            </div>
                                        {/if}
                                    </div>
                                {/each}
                            </div>
                        {/if}
                        {#if t.can_manage && requestable.length}
                            <div class="card-footer">
                                <div class="input-group">
                                    <select class="form-select" bind:value={robotPick[t.ID]} aria-label="Request a robot for {t.name}">
                                        <option value={undefined}>Request a robot...</option>
                                        {#each requestable as r (r.id)}
                                            <option value={r.id}>{r.name}</option>
                                        {/each}
                                    </select>
                                    <button class="btn" type="button" disabled={busy || !robotPick[t.ID]} on:click={() => { send('POST', `/api/threads/${t.ID}/resources`, { resource_id: robotPick[t.ID] }); robotPick[t.ID] = undefined; }}>Request</button>
                                </div>
                            </div>
                        {/if}
                    </div>
                </div>
            {/each}
        </div>
    {/if}

    {#if others.length}
        <details class="card fold" open={!loading && mineList.length === 0}>
            <summary class="card-header">
                <h3 class="card-title d-flex align-items-center gap-2">
                    Other threads <span class="badge bg-secondary-lt">{othersShown.length}</span>
                </h3>
                <span class="card-actions text-secondary"><IconChevronDown size={18} class="icon chevron" /></span>
            </summary>
            <div class="list-group list-group-flush">
                {#each othersShown as t (t.ID)}
                    <div class="list-group-item">
                        <div class="row g-1 align-items-center">
                            <div class="col-12 col-md d-flex align-items-center min-w-0">
                                <span class="dot" style="--hue: {hueFor(t.ID)}"></span>
                                <strong class="text-truncate">{t.name}</strong>
                                <span class="text-secondary ms-2 text-nowrap">{t.lead_name}</span>
                            </div>
                            <div class="col-12 col-md-auto small text-secondary">
                                {t.access.filter((a) => a.status === 'approved').map((a) => a.resource_name).join(', ') || 'No robots'}
                            </div>
                        </div>
                    </div>
                {:else}
                    <div class="list-group-item text-secondary small">None match.</div>
                {/each}
            </div>
        </details>
    {/if}
</div>

<style>
    /* A thread's or robot's colour (lib/palette.js), the same on the calendar */
    .dot {
        display: inline-block;
        flex: none;
        width: 0.6rem;
        height: 0.6rem;
        margin-right: 0.5rem;
        border-radius: 50%;
        background: var(--hue);
    }

    .hue-bg {
        background: var(--hue);
    }

    .inactive {
        opacity: 0.7;
    }

    .min-w-0 {
        min-width: 0;
    }

    .hours-group {
        width: auto;
        max-width: 15rem;
        flex-wrap: nowrap;
    }

    .hours-group .form-control {
        min-width: 5.5rem;
        max-width: 7rem;
    }

    /* Native <details> folds, dressed as cards */
    .fold > summary {
        cursor: pointer;
        list-style: none;
        min-height: 3.25rem;
    }

    .fold > summary::-webkit-details-marker {
        display: none;
    }

    .fold:not([open]) > summary {
        border-bottom: 0;
    }

    .fold :global(.chevron) {
        transition: transform 0.2s;
    }

    .fold[open] :global(.chevron) {
        transform: rotate(180deg);
    }

    .search {
        position: relative;
        min-width: min(100%, 20rem);
    }

    .search .form-control {
        padding-right: 2.75rem;
    }

    .clear-search {
        position: absolute;
        top: 50%;
        right: 0.25rem;
        z-index: 11;
        transform: translateY(-50%);
    }
</style>
