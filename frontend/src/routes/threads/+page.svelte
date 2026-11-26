<script>
    // Research threads: a prof's project, its members, and the robots a lab
    // admin has signed off for it. Members book those robots for the thread.
    // The server decides who may change what; the page hides what it'd refuse.
    import { onMount } from 'svelte';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import { hueFor } from '$lib/palette.js';

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

<div class="container">
    <div class="page-header">
        <div>
            <h1>🧪 Research Threads</h1>
            <p class="subtitle">A thread is a project. Its members book the robots a lab admin has signed off for it.</p>
        </div>
        <a class="submit-btn" href="/mechlab">🦾 Book a robot</a>
    </div>

    {#if message}<div class="message {messageType}" role="status">{message}</div>{/if}

    <div class="stack">
        {#if can(user, 'create_threads')}
            <details class="panel new-thread" open={!loading && mineList.length === 0}>
                <summary><span class="plus" aria-hidden="true">＋</span> Start a new thread</summary>
                <form class="new-form" on:submit|preventDefault={create}>
                    <div class="form-group">
                        <label for="nt-name">Name</label>
                        <input id="nt-name" bind:value={newThread.name} maxlength="100" required placeholder="e.g. Grasping" />
                    </div>
                    <div class="form-group grow">
                        <label for="nt-desc">Description <span class="optional-tag">(optional)</span></label>
                        <input id="nt-desc" bind:value={newThread.description} maxlength="1000" placeholder="What the project is about" />
                    </div>
                    <button class="submit-btn" type="submit" disabled={busy}>{busy ? 'Creating…' : 'Create thread'}</button>
                </form>
                <p class="help-text">You lead the threads you create: add members, then request robot access for a lab admin to sign off.</p>
            </details>
        {/if}

        {#if labAdmin && waiting.length}
            <section class="panel signoff">
                <h3 class="section-title" style="--hue: var(--rp-gold)">⏳ Waiting for your sign-off <span class="count">{waiting.length}</span></h3>
                {#each waiting as { t, a } (key(t, a))}
                    <div class="list-row">
                        <span class="what"><span class="dot" style="--hue: {hueFor(t.ID)}"></span> <strong>{t.name}</strong> <span class="muted">wants</span> {a.resource_name}</span>
                        <span class="buttons">
                            <span class="hours-field">
                                <input class="hours mono" type="number" min="0" max="168" bind:value={hours[key(t, a)]} placeholder="no limit" aria-label="Hours per week for {t.name} on {a.resource_name}" />
                                <span class="muted">h/week</span>
                            </span>
                            <button class="submit-btn" disabled={busy} on:click={() => decide(t, a, 'approve')}>Approve</button>
                            <button class="ghost-btn" disabled={busy} on:click={() => decide(t, a, 'decline')}>Decline</button>
                        </span>
                    </div>
                {/each}
            </section>
        {/if}

        <div class="list-head">
            <h2>{labAdmin ? 'All threads' : 'My threads'} {#if !loading}<span class="count">{mineList.length}</span>{/if}</h2>
            {#if threads.length > 3}
                <div class="search-input-group">
                    <input type="search" bind:value={query} placeholder="🔍 Find a thread, person or robot" aria-label="Find a thread" />
                    {#if query}<button class="clear-search-btn" on:click={() => (query = '')} aria-label="Clear search">×</button>{/if}
                </div>
            {/if}
        </div>

        {#if loading}
            <p class="note">Loading…</p>
        {:else if mineList.length === 0}
            <div class="panel empty-state">
                <p class="empty-icon" aria-hidden="true">🧪</p>
                <p>You're not in a research thread yet. Ask your prof to add you{others.length ? ' - here is who leads what.' : '.'}</p>
            </div>
        {:else if mineShown.length === 0}
            <p class="note">No threads match “{query}”. <button class="link-btn" on:click={() => (query = '')}>Clear</button></p>
        {/if}

        {#if mineShown.length}
            <div class="threads">
                {#each mineShown as t (t.ID)}
                    {@const memberIds = t.members.map((m) => m.id)}
                    {@const requestable = robots.filter((r) => !t.access.some((a) => a.resource_id === r.id))}
                    <section class="thread-card" class:inactive={!t.active} style="--hue: {hueFor(t.ID)}">
                        {#if editing === t.ID}
                            <form class="edit-form" on:submit|preventDefault={() => saveEdit(t)}>
                                <div class="form-group">
                                    <label for="ed-name-{t.ID}">Name</label>
                                    <input id="ed-name-{t.ID}" bind:value={edit.name} maxlength="100" required />
                                </div>
                                <div class="form-group">
                                    <label for="ed-desc-{t.ID}">Description</label>
                                    <input id="ed-desc-{t.ID}" bind:value={edit.description} maxlength="1000" placeholder="Description" />
                                </div>
                                <div class="buttons">
                                    <button class="submit-btn" type="submit" disabled={busy}>Save</button>
                                    <button class="ghost-btn" type="button" on:click={() => (editing = null)}>Cancel</button>
                                </div>
                            </form>
                        {:else}
                            <div class="card-head">
                                <h3>{t.name}</h3>
                                {#if !t.active}<span class="status-badge">Inactive</span>{/if}
                                {#if t.mine && !t.can_manage}<span class="status-badge member">Member</span>{/if}
                                <span class="buttons end">
                                    {#if labAdmin}
                                        <label class="toggle"><input type="checkbox" checked={t.active} disabled={busy} on:change={(e) => setActive(t, e.currentTarget.checked)} /> Active</label>
                                    {/if}
                                    {#if t.can_manage}<button class="ghost-btn" on:click={() => startEdit(t)}>✏️ Edit</button>{/if}
                                </span>
                            </div>
                            {#if t.description}<p class="desc">{t.description}</p>{/if}
                        {/if}
                        <p class="lead">Led by <strong>{t.lead_name}</strong></p>

                        <h4 class="sub-title">👥 Members <span class="mono muted">{t.members.length}</span></h4>
                        <div class="chip-row">
                            {#each t.members as m (m.id)}
                                <span class="member-chip" class:is-lead={m.id === t.lead_id}>
                                    {m.name}{#if m.id === t.lead_id}<span class="lead-tag">lead</span>{/if}
                                    {#if t.can_manage && m.id !== t.lead_id}
                                        <button class="x" aria-label="Remove {m.name}" disabled={busy} on:click={() => removeMember(t, m, memberIds)}>×</button>
                                    {/if}
                                </span>
                            {/each}
                        </div>
                        {#if t.can_manage && people.length}
                            <div class="inline">
                                <select bind:value={addPick[t.ID]} aria-label="Add member to {t.name}">
                                    <option value={undefined}>Add member…</option>
                                    {#each people.filter((p) => !memberIds.includes(p.id)) as p (p.id)}
                                        <option value={p.id}>{p.name}</option>
                                    {/each}
                                </select>
                                <button class="ghost-btn" disabled={busy || !addPick[t.ID]} on:click={() => { setMembers(t, [...memberIds, addPick[t.ID]]); addPick[t.ID] = undefined; }}>Add</button>
                            </div>
                        {/if}

                        <h4 class="sub-title">🦾 Robots</h4>
                        {#if t.access.length === 0}<p class="muted small">No robot access yet.{#if t.can_manage && requestable.length}{' Request one below.'}{/if}</p>{/if}
                        {#each t.access as a (a.id)}
                            <div class="robot">
                                <div class="robot-head">
                                    <span class="robot-name"><span class="dot" style="--hue: {hueFor(a.resource_id)}"></span><strong>{a.resource_name}</strong></span>
                                    {#if a.status === 'pending'}
                                        <span class="status-badge warn">Waiting for sign-off</span>
                                    {:else}
                                        <span class="status-badge ok">Approved</span>
                                    {/if}
                                </div>
                                {#if a.status !== 'pending'}
                                    {#if a.hours_per_week}<div class="bar" class:over={usedPct(a) >= 100}><span style="width: {usedPct(a)}%"></span></div>{/if}
                                    <p class="mono muted small">{usage(a)}</p>
                                {/if}
                                {#if a.status === 'approved' || t.can_manage}
                                <div class="buttons">
                                    {#if labAdmin && a.status === 'approved'}
                                        <span class="hours-field">
                                            <input class="hours mono" type="number" min="0" max="168" bind:value={hours[key(t, a)]} placeholder="no limit" aria-label="Hours per week for {a.resource_name}" />
                                            <span class="muted">h/week</span>
                                        </span>
                                        <button class="ghost-btn" disabled={busy} on:click={() => decide(t, a, 'approve')}>Save</button>
                                    {/if}
                                    {#if a.status === 'approved' && t.active}<a class="ghost-btn" href="/mechlab">Book</a>{/if}
                                    {#if t.can_manage}<button class="danger-btn" disabled={busy} on:click={() => removeAccess(t, a)}>Remove</button>{/if}
                                </div>
                                {/if}
                            </div>
                        {/each}
                        {#if t.can_manage && requestable.length}
                            <div class="inline">
                                <select bind:value={robotPick[t.ID]} aria-label="Request a robot for {t.name}">
                                    <option value={undefined}>Request a robot…</option>
                                    {#each requestable as r (r.id)}
                                        <option value={r.id}>{r.name}</option>
                                    {/each}
                                </select>
                                <button class="ghost-btn" disabled={busy || !robotPick[t.ID]} on:click={() => { send('POST', `/api/threads/${t.ID}/resources`, { resource_id: robotPick[t.ID] }); robotPick[t.ID] = undefined; }}>Request</button>
                            </div>
                        {/if}
                    </section>
                {/each}
            </div>
        {/if}

        {#if others.length}
            <details class="panel others" open={!loading && mineList.length === 0}>
                <summary>Other threads <span class="count">{othersShown.length}</span></summary>
                {#each othersShown as t (t.ID)}
                    <div class="list-row">
                        <span class="what"><span class="dot" style="--hue: {hueFor(t.ID)}"></span> <strong>{t.name}</strong> <span class="muted">· {t.lead_name}</span></span>
                        <span class="muted small">{t.access.filter((a) => a.status === 'approved').map((a) => a.resource_name).join(', ') || 'No robots'}</span>
                    </div>
                {:else}
                    <p class="muted small">None match.</p>
                {/each}
            </details>
        {/if}
    </div>
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
        color: var(--rp-rose);
    }

    .subtitle {
        margin: 2px 0 0;
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
    }

    .stack {
        display: flex;
        flex-direction: column;
        gap: 16px;
    }

    .panel,
    .thread-card {
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--rp-text) 4%, transparent), transparent 60%),
            var(--card);
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        padding: clamp(15px, 3vw, 22px);
        box-shadow: var(--shadow-sm);
        min-width: 0;
    }

    .section-title {
        margin: 0 0 10px;
        font-size: 1.05rem;
        color: var(--ctp-text);
        border-left: 4px solid var(--hue);
        padding-left: 12px;
    }

    .count {
        display: inline-block;
        margin-left: 6px;
        padding: 2px 8px;
        border-radius: 12px;
        background: var(--ctp-surface0);
        color: var(--ctp-subtext0);
        font-size: 0.75rem;
        font-weight: 600;
        vertical-align: 2px;
    }

    /* --- new thread ------------------------------------------------------ */

    .new-thread summary {
        display: flex;
        align-items: center;
        gap: 10px;
        min-height: 40px;
        list-style: none;
        cursor: pointer;
        font-weight: 650;
        color: var(--rp-rose);
    }

    .new-thread summary::-webkit-details-marker {
        display: none;
    }

    .plus {
        display: grid;
        place-items: center;
        width: 30px;
        height: 30px;
        border-radius: 8px;
        background: color-mix(in srgb, var(--rp-rose) 18%, transparent);
        transition: transform var(--normal) var(--ease);
    }

    .new-thread[open] .plus {
        transform: rotate(45deg);
    }

    .new-form {
        display: flex;
        gap: 12px;
        align-items: flex-end;
        flex-wrap: wrap;
        margin-top: 14px;
    }

    .new-form .form-group {
        flex: 1 1 200px;
        margin: 0;
    }

    .new-form .form-group.grow {
        flex: 2 1 260px;
    }

    .help-text {
        margin: 10px 0 0;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
    }

    .optional-tag {
        font-size: 0.78rem;
        font-weight: 400;
        color: var(--ctp-overlay1);
    }

    .form-group {
        margin-bottom: 12px;
    }

    .form-group label {
        display: block;
        margin-bottom: 6px;
        font-weight: 600;
        font-size: 0.9rem;
        color: var(--ctp-text);
    }

    input:not([type='checkbox']),
    select {
        width: 100%;
        min-height: 42px;
        padding: 8px 12px;
        border: 2px solid var(--ctp-surface0);
        border-radius: 8px;
        background: var(--bg);
        color: var(--ctp-text);
        font: inherit;
        box-sizing: border-box;
        transition: border-color var(--fast) var(--ease), box-shadow var(--fast) var(--ease);
    }

    input:not([type='checkbox']):focus,
    select:focus {
        border-color: var(--rp-rose);
        outline: none;
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--rp-rose) 25%, transparent);
    }

    /* --- lists ------------------------------------------------------------ */

    .list-row {
        display: flex;
        justify-content: space-between;
        align-items: center;
        flex-wrap: wrap;
        gap: 10px;
        padding: 10px 0;
        border-top: 1px solid var(--ctp-surface0);
    }

    .section-title + .list-row,
    summary + .list-row {
        border-top: none;
    }

    .what {
        min-width: 0;
    }

    .list-head {
        display: flex;
        justify-content: space-between;
        align-items: center;
        flex-wrap: wrap;
        gap: 10px;
        margin-top: 4px;
    }

    .list-head h2 {
        margin: 0;
        font-size: 1.2rem;
        font-weight: 600;
        color: var(--ctp-text);
    }

    .search-input-group {
        position: relative;
        flex: 0 1 340px;
        min-width: 0;
    }

    .search-input-group input {
        padding-right: 46px;
    }

    .clear-search-btn {
        position: absolute;
        right: 7px;
        top: 50%;
        transform: translateY(-50%);
        width: 28px;
        height: 28px;
        border: none;
        border-radius: 50%;
        background: var(--rp-love);
        color: var(--accent-ink);
        font-weight: bold;
        cursor: pointer;
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

    .empty-state {
        text-align: center;
        color: var(--ctp-subtext0);
    }

    .empty-icon {
        font-size: 2.2rem;
        margin: 0 0 4px;
    }

    .empty-state p {
        margin: 0;
    }

    .others summary {
        min-height: 36px;
        cursor: pointer;
        font-weight: 600;
        color: var(--ctp-text);
    }

    /* --- thread cards ----------------------------------------------------- */

    .threads {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(min(100%, 360px), 1fr));
        gap: clamp(12px, 2vw, 18px);
        align-items: start;
    }

    .thread-card {
        position: relative;
        overflow: hidden;
        border-color: color-mix(in srgb, var(--hue) 30%, var(--ctp-surface0));
        transition: transform var(--normal) var(--ease), box-shadow var(--normal) var(--ease), border-color var(--normal) var(--ease);
    }

    /* The thread's colour down the leading edge, as on the home tiles */
    .thread-card::after {
        content: '';
        position: absolute;
        left: 0;
        top: 0;
        bottom: 0;
        width: 4px;
        background: var(--hue);
    }

    .thread-card:hover {
        border-color: color-mix(in srgb, var(--hue) 55%, var(--ctp-surface1));
        box-shadow: var(--shadow);
    }

    .thread-card.inactive {
        opacity: 0.65;
    }

    .card-head {
        display: flex;
        align-items: center;
        gap: 8px;
        flex-wrap: wrap;
    }

    .card-head h3 {
        margin: 0;
        font-size: 1.15rem;
        font-weight: 650;
        color: var(--hue);
        overflow-wrap: anywhere;
    }

    :root[data-theme='light'] .card-head h3 {
        color: color-mix(in srgb, var(--hue) 75%, var(--rp-text));
    }

    .end {
        margin-left: auto;
    }

    .desc {
        margin: 8px 0 0;
        line-height: 1.45;
        color: var(--ctp-text);
    }

    .lead {
        margin: 6px 0 0;
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
    }

    .sub-title {
        margin: 18px 0 8px;
        font-size: 0.88rem;
        font-weight: 650;
        color: var(--ctp-subtext0);
    }

    .chip-row {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
    }

    .member-chip {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        min-height: 32px;
        padding: 2px 12px;
        border-radius: 999px;
        background: var(--ctp-surface0);
        border: 1px solid var(--ctp-surface1);
        font-size: 0.88rem;
        color: var(--ctp-text);
    }

    .member-chip.is-lead {
        border-color: color-mix(in srgb, var(--hue) 55%, transparent);
        background: color-mix(in srgb, var(--hue) 14%, var(--card));
    }

    .lead-tag {
        font-size: 0.7rem;
        font-weight: 700;
        text-transform: uppercase;
        letter-spacing: 0.04em;
        color: var(--ctp-subtext0);
    }

    .member-chip .x {
        display: grid;
        place-items: center;
        width: 30px;
        height: 30px;
        margin: -2px -10px -2px -4px;
        padding: 0;
        border: none;
        border-radius: 50%;
        background: none;
        color: var(--ctp-subtext0);
        font: inherit;
        font-size: 1.15rem;
        cursor: pointer;
    }

    .member-chip .x:hover {
        color: var(--danger);
        background: color-mix(in srgb, var(--danger) 12%, transparent);
    }

    .inline {
        display: flex;
        gap: 8px;
        align-items: center;
        margin-top: 10px;
    }

    .inline select {
        flex: 1;
        min-width: 0;
    }

    .buttons {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
        align-items: center;
    }

    .hours-field {
        display: inline-flex;
        align-items: center;
        gap: 6px;
    }

    .hours {
        width: 96px !important;
    }

    .robot {
        display: flex;
        flex-direction: column;
        gap: 8px;
        padding: 12px 0;
        border-top: 1px solid var(--ctp-surface0);
    }

    .robot-head {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 8px;
    }

    .robot-name {
        display: inline-flex;
        align-items: center;
        gap: 8px;
        min-width: 0;
    }

    .bar {
        height: 8px;
        border-radius: 999px;
        background: var(--ctp-surface0);
        overflow: hidden;
    }

    .bar > span {
        display: block;
        height: 100%;
        border-radius: inherit;
        background: linear-gradient(90deg, var(--rp-foam), var(--rp-iris));
    }

    .bar.over > span {
        background: var(--warn);
    }

    .dot {
        display: inline-block;
        flex: none;
        width: 9px;
        height: 9px;
        border-radius: 50%;
        background: var(--hue);
    }

    .status-badge {
        display: inline-block;
        padding: 2px 10px;
        border-radius: 20px;
        font-size: 0.75rem;
        font-weight: 700;
        white-space: nowrap;
        background: var(--ctp-surface0);
        color: var(--ctp-subtext0);
    }

    .status-badge.ok {
        background: color-mix(in srgb, var(--rp-foam) 22%, transparent);
        color: var(--rp-foam);
    }

    .status-badge.warn {
        background: color-mix(in srgb, var(--rp-gold) 22%, transparent);
        color: var(--rp-gold);
    }

    .status-badge.member {
        background: color-mix(in srgb, var(--hue) 18%, transparent);
        color: var(--ctp-text);
    }

    :root[data-theme='light'] .status-badge.ok {
        color: var(--rp-pine);
    }

    :root[data-theme='light'] .status-badge.warn {
        color: color-mix(in srgb, var(--rp-gold) 60%, var(--rp-text));
    }

    .toggle {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        min-height: 40px;
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
        cursor: pointer;
    }

    .toggle input {
        width: 18px;
        height: 18px;
        accent-color: var(--rp-rose);
    }

    /* --- buttons -------------------------------------------------------- */

    .submit-btn,
    .ghost-btn,
    .danger-btn {
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
        white-space: nowrap;
        transition: transform var(--fast) var(--ease), box-shadow var(--fast) var(--ease), background var(--fast) var(--ease);
    }

    .submit-btn {
        border: none;
        background: linear-gradient(135deg, var(--rp-rose), var(--rp-love));
        color: var(--accent-ink);
    }

    .submit-btn:hover:not(:disabled) {
        transform: translateY(-1px);
        box-shadow: 0 6px 18px color-mix(in srgb, var(--rp-rose) 30%, transparent);
    }

    .ghost-btn {
        border: 1px solid var(--ctp-surface1);
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        font-weight: 500;
    }

    .ghost-btn:hover:not(:disabled) {
        background: var(--ctp-surface1);
    }

    .danger-btn {
        border: 1px solid color-mix(in srgb, var(--danger) 45%, transparent);
        background: color-mix(in srgb, var(--danger) 10%, transparent);
        color: var(--danger);
        font-weight: 500;
    }

    .danger-btn:hover:not(:disabled) {
        background: color-mix(in srgb, var(--danger) 18%, transparent);
    }

    button:disabled {
        opacity: 0.55;
        cursor: not-allowed;
    }

    .muted {
        color: var(--ctp-subtext0);
    }

    .mono {
        font-family: var(--mono);
        font-variant-numeric: tabular-nums;
    }

    .small {
        margin: 0;
        font-size: 0.85rem;
    }

    .note {
        margin: 0;
        color: var(--ctp-subtext0);
    }

    .message.success {
        background: color-mix(in srgb, var(--rp-foam) 15%, var(--card));
    }

    .message.error {
        background: color-mix(in srgb, var(--rp-love) 15%, var(--card));
    }

    @media (max-width: 520px) {
        .page-header .submit-btn,
        .new-form .submit-btn {
            width: 100%;
        }

        .search-input-group {
            flex-basis: 100%;
        }
    }
</style>
