<script>
    // Research threads: a prof's project, its members, and the robots a lab
    // admin has signed off for it. Members book those robots for the thread.
    // The server decides who may change what; the page hides what it'd refuse.
    import { onMount } from 'svelte';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import Icon from '$lib/Icon.svelte';
    import { AREA, hueFor } from '$lib/palette.js';

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

    $: labAdmin = can(user, 'approve_mech_bookings');
    $: mineList = threads.filter((t) => t.mine || t.can_manage);
    $: others = threads.filter((t) => !t.mine && !t.can_manage);
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
        // Only people who can manage threads may list everyone; others just don't get the picker
        api('/api/admin/people').then((r) => (r.ok ? r.json() : [])).then((p) => (people = p)).catch(() => {});
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

<svelte:head><title>Research threads · RRC</title></svelte:head>

<div class="page">
    <header class="page-head">
        <div class="title">
            <span class="icon-badge" style="--hue: {AREA.threads}"><Icon name="users" /></span>
            <div>
                <h1>Research threads</h1>
                <p>A thread is a project. Its members book the robots a lab admin has signed off for it.</p>
            </div>
        </div>
        <a class="btn" href="/mechlab">Book a robot</a>
    </header>

    {#if message}<div class="message {messageType}" role="status">{message}</div>{/if}

    <div class="stack">
        {#if can(user, 'create_threads')}
            <details class="card new-thread" open={!loading && mineList.length === 0}>
                <summary class="btn">New thread</summary>
                <form class="inline" on:submit|preventDefault={create}>
                    <input bind:value={newThread.name} maxlength="100" required placeholder="Name, e.g. Grasping" aria-label="Thread name" />
                    <input bind:value={newThread.description} maxlength="1000" placeholder="Description (optional)" aria-label="Description" />
                    <button class="btn primary" type="submit" disabled={busy}>{busy ? 'Creating…' : 'Create'}</button>
                </form>
            </details>
        {/if}

        {#if labAdmin && waiting.length}
            <section class="card">
                <h2 class="label">Waiting for your sign-off</h2>
                {#each waiting as { t, a } (key(t, a))}
                    <div class="row">
                        <span><span class="dot" style="--hue: {hueFor(t.ID)}"></span> <strong>{t.name}</strong> <span class="muted">wants</span> {a.resource_name}</span>
                        <span class="buttons">
                            <input class="hours mono" type="number" min="0" max="168" bind:value={hours[key(t, a)]} placeholder="no limit" aria-label="Hours per week" />
                            <span class="muted">h/week</span>
                            <button class="btn primary" disabled={busy} on:click={() => decide(t, a, 'approve')}>Approve</button>
                            <button class="btn" disabled={busy} on:click={() => decide(t, a, 'decline')}>Decline</button>
                        </span>
                    </div>
                {/each}
            </section>
        {/if}

        <h2 class="section">{labAdmin ? 'All threads' : 'My threads'}</h2>
        {#if loading}
            <p class="note">Loading…</p>
        {:else if mineList.length === 0}
            <p class="note">You're not in a research thread yet. Ask your prof to add you{others.length ? '. Here is who leads what.' : '.'}</p>
        {/if}

        {#if mineList.length}
            <div class="grid threads">
                {#each mineList as t (t.ID)}
                    {@const memberIds = t.members.map((m) => m.id)}
                    {@const requestable = robots.filter((r) => !t.access.some((a) => a.resource_id === r.id))}
                    <section class="card hued" class:inactive={!t.active} style="--hue: {hueFor(t.ID)}">
                        {#if editing === t.ID}
                            <form class="inline" on:submit|preventDefault={() => saveEdit(t)}>
                                <input bind:value={edit.name} maxlength="100" required aria-label="Thread name" />
                                <input bind:value={edit.description} maxlength="1000" placeholder="Description" aria-label="Description" />
                                <button class="btn primary" type="submit" disabled={busy}>Save</button>
                                <button class="btn" type="button" on:click={() => (editing = null)}>Cancel</button>
                            </form>
                        {:else}
                            <div class="card-head">
                                <h3>{t.name}</h3>
                                {#if !t.active}<span class="chip">Inactive</span>{/if}
                                <span class="buttons end">
                                    {#if labAdmin}
                                        <label class="toggle"><input type="checkbox" checked={t.active} on:change={(e) => setActive(t, e.currentTarget.checked)} /> Active</label>
                                    {/if}
                                    {#if t.can_manage}<button class="btn" on:click={() => startEdit(t)}>Edit</button>{/if}
                                </span>
                            </div>
                            {#if t.description}<p class="desc">{t.description}</p>{/if}
                        {/if}
                        <p class="muted lead">Led by {t.lead_name}</p>

                        <h4 class="label">Members</h4>
                        <div class="chips">
                            {#each t.members as m (m.id)}
                                <span class="chip hue">
                                    {m.name}
                                    {#if t.can_manage && m.id !== t.lead_id}
                                        <button class="x" aria-label="Remove {m.name}" on:click={() => removeMember(t, m, memberIds)}>×</button>
                                    {/if}
                                </span>
                            {/each}
                        </div>
                        {#if t.can_manage && people.length}
                            <div class="inline">
                                <select bind:value={addPick[t.ID]} aria-label="Add member">
                                    <option value={undefined}>Add member…</option>
                                    {#each people.filter((p) => !memberIds.includes(p.id)) as p (p.id)}
                                        <option value={p.id}>{p.name}</option>
                                    {/each}
                                </select>
                                <button class="btn" disabled={busy || !addPick[t.ID]} on:click={() => { setMembers(t, [...memberIds, addPick[t.ID]]); addPick[t.ID] = undefined; }}>Add</button>
                            </div>
                        {/if}

                        <h4 class="label">Robots</h4>
                        {#if t.access.length === 0}<p class="muted small">No robot access yet.</p>{/if}
                        {#each t.access as a (a.id)}
                            <div class="robot">
                                <div class="robot-head">
                                    <span class="robot-name"><span class="dot" style="--hue: {hueFor(a.resource_id)}"></span><strong>{a.resource_name}</strong></span>
                                    {#if a.status === 'pending'}
                                        <span class="chip warn">Waiting for sign-off</span>
                                    {:else}
                                        <span class="chip ok">Approved</span>
                                    {/if}
                                </div>
                                {#if a.status !== 'pending'}
                                    {#if a.hours_per_week}<div class="bar"><span style="width: {usedPct(a)}%;{usedPct(a) >= 100 ? ' --hue: var(--warn);' : ''}"></span></div>{/if}
                                    <p class="mono muted small">{usage(a)}</p>
                                {/if}
                                {#if (labAdmin && a.status === 'approved') || t.can_manage}
                                    <div class="buttons">
                                        {#if labAdmin && a.status === 'approved'}
                                            <input class="hours mono" type="number" min="0" max="168" bind:value={hours[key(t, a)]} placeholder="no limit" aria-label="Hours per week" />
                                            <span class="muted">h/week</span>
                                            <button class="btn" disabled={busy} on:click={() => decide(t, a, 'approve')}>Save</button>
                                        {/if}
                                        {#if t.can_manage}<button class="btn danger" on:click={() => removeAccess(t, a)}>Remove</button>{/if}
                                    </div>
                                {/if}
                            </div>
                        {/each}
                        {#if t.can_manage && requestable.length}
                            <div class="inline">
                                <select bind:value={robotPick[t.ID]} aria-label="Request a robot">
                                    <option value={undefined}>Request a robot…</option>
                                    {#each requestable as r (r.id)}
                                        <option value={r.id}>{r.name}</option>
                                    {/each}
                                </select>
                                <button class="btn" disabled={busy || !robotPick[t.ID]} on:click={() => { send('POST', `/api/threads/${t.ID}/resources`, { resource_id: robotPick[t.ID] }); robotPick[t.ID] = undefined; }}>Request</button>
                            </div>
                        {/if}
                    </section>
                {/each}
            </div>
        {/if}

        {#if others.length}
            <details class="card others" open={!loading && mineList.length === 0}>
                <summary class="label">Other threads <span class="mono">({others.length})</span></summary>
                {#each others as t (t.ID)}
                    <div class="row">
                        <span><span class="dot" style="--hue: {hueFor(t.ID)}"></span> <strong>{t.name}</strong> <span class="muted">· {t.lead_name}</span></span>
                        <span class="muted small">{t.access.filter((a) => a.status === 'approved').map((a) => a.resource_name).join(', ') || 'No robots'}</span>
                    </div>
                {/each}
            </details>
        {/if}
    </div>
</div>

<style>
    .stack {
        display: flex;
        flex-direction: column;
        gap: 14px;
    }

    .threads {
        grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr));
        align-items: start;
    }

    .section {
        margin: 10px 0 0;
        font-size: 1.05rem;
        font-weight: 600;
    }

    .row {
        flex-wrap: wrap;
    }

    .inline,
    .buttons {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
        align-items: center;
    }

    .inline {
        margin-top: 10px;
    }

    .inline input,
    .inline select {
        flex: 1;
        min-width: 160px;
    }

    input,
    select {
        min-height: 40px;
        box-sizing: border-box;
        padding: 6px 10px;
    }

    .hours {
        width: 80px;
    }

    .new-thread summary {
        list-style: none;
    }

    .new-thread summary::-webkit-details-marker {
        display: none;
    }

    .new-thread:not([open]) {
        padding: 12px;
    }

    .others summary {
        margin: 0;
        cursor: pointer;
    }

    .others[open] summary {
        margin-bottom: 6px;
    }

    .card.inactive {
        opacity: 0.6;
    }

    .card-head {
        display: flex;
        align-items: center;
        gap: 8px;
        flex-wrap: wrap;
    }

    .card-head h3 {
        margin: 0;
        font-size: 1.1rem;
        font-weight: 600;
    }

    .end {
        margin-left: auto;
    }

    .desc {
        margin: 8px 0 0;
        line-height: 1.45;
    }

    .lead {
        margin: 6px 0 0;
        font-size: 0.9rem;
    }

    h4.label {
        margin: 18px 0 8px;
    }

    .small {
        margin: 0;
        font-size: 0.85rem;
    }

    .chips {
        display: flex;
        gap: 6px;
        flex-wrap: wrap;
    }

    .chip .x {
        min-width: 28px;
        min-height: 28px;
        margin: -4px -8px -4px -4px;
        padding: 0;
        border: none;
        background: none;
        color: var(--muted);
        font: inherit;
        font-size: 1.1rem;
        cursor: pointer;
    }

    .chip .x:hover {
        color: var(--danger);
    }

    .robot {
        display: flex;
        flex-direction: column;
        gap: 8px;
        padding: 12px 0;
        border-top: 1px solid var(--line);
    }

    .title {
        display: flex;
        align-items: center;
        gap: 12px;
    }

    .robot-name {
        display: inline-flex;
        align-items: center;
        gap: 8px;
    }

    .robot-head {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 8px;
    }

    .toggle {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        font-size: 0.9rem;
        color: var(--muted);
    }

    .toggle input {
        min-height: 0;
    }

    .note {
        margin: 0;
        color: var(--muted);
    }
</style>
