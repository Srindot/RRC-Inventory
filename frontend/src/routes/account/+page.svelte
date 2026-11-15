<script>
    import { onMount } from 'svelte';
    import { api, clearSession, loginUrl, refreshUser } from '$lib/auth.js';
    import Icon from '$lib/Icon.svelte';
    import { AREA, hueFor } from '$lib/palette.js';

    let user = null;
    let loans = [];
    let bookings = [];
    let custodianItems = [];
    let notice = '';
    let noticeError = false;
    let pw = { old_password: '', new_password: '', confirm: '' };
    let pwMessage = '';
    let pwError = false;
    let busy = false;

    $: initial = (user?.name || '?').trim().charAt(0).toUpperCase();
    $: openLoans = loans.filter((l) => l.status === 'active' || l.status === 'pending');
    $: overdue = openLoans.filter((l) => l.overdue);
    $: upcoming = bookings.filter((b) => new Date(b.end_time) > new Date() && b.status !== 'rejected');
    // Tracked items not confirmed in the last month
    $: toVerify = custodianItems.filter((i) => !i.last_verified_at || Date.now() - new Date(i.last_verified_at) > 30 * 864e5);

    onMount(async () => {
        user = await refreshUser();
        if (!user) {
            location.href = loginUrl();
            return;
        }
        load();
    });

    async function getJSON(path) {
        const response = await api(path).catch(() => null);
        return response?.ok ? response.json() : [];
    }

    async function load() {
        [loans, bookings, custodianItems] = await Promise.all([
            getJSON('/api/loans/mine'),
            getJSON('/api/bookings/mine'),
            getJSON('/api/items/custodian')
        ]);
    }

    async function post(path, body) {
        try {
            const response = await api(path, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body || {})
            });
            const result = await response.json().catch(() => ({}));
            if (response.status === 401) {
                location.href = loginUrl();
                return;
            }
            noticeError = !response.ok;
            notice = result.message || result.error || `HTTP ${response.status}`;
            setTimeout(() => (notice = ''), 5000);
            load();
        } catch {
            noticeError = true;
            notice = 'Could not reach the server.';
        }
    }

    async function changePassword() {
        pwMessage = '';
        pwError = true;
        if (pw.new_password !== pw.confirm) {
            pwMessage = 'The new passwords do not match';
            return;
        }
        busy = true;
        try {
            const response = await api('/api/auth/change-password', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ old_password: pw.old_password, new_password: pw.new_password })
            });
            const body = await response.json().catch(() => ({}));
            pwError = !response.ok;
            pwMessage = body.error || body.message || `HTTP ${response.status}`;
            if (response.ok) pw = { old_password: '', new_password: '', confirm: '' };
        } catch {
            pwMessage = 'Could not reach the server.';
        } finally {
            busy = false;
        }
    }

    async function logout() {
        await api('/api/auth/logout', { method: 'POST' }).catch(() => {});
        clearSession();
        location.href = '/';
    }

    const when = (b) => `${new Date(b.start_time).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}, ` +
        `${new Date(b.start_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} - ` +
        `${new Date(b.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`;
</script>

<svelte:head><title>My account · RRC</title></svelte:head>

<div class="page">
    {#if user}
        <header class="page-head">
            <div class="who">
                <span class="avatar">{initial}</span>
                <div>
                    <h1>{user.name}</h1>
                    <p>{user.group} · {user.email}</p>
                </div>
            </div>
            <button class="btn danger" on:click={logout}>Log out</button>
        </header>

        {#if notice}<p class="message {noticeError ? 'error' : 'success'}" role="status">{notice}</p>{/if}

        <div class="grid">
            {#each overdue as loan (loan.ID)}
                <div class="card overdue">
                    <p class="label">Overdue</p>
                    <h2>{loan.item_name} is {loan.days_overdue ? `${loan.days_overdue} day${loan.days_overdue === 1 ? '' : 's'} ` : ''}overdue</h2>
                    <p class="muted">What happened? Your answer keeps the records right{loan.days_overdue >= 7 ? ' and lifts the pause on borrowing and booking' : ''}.</p>
                    <div class="actions">
                        <button class="btn" on:click={() => post(`/api/loans/${loan.ID}/overdue-answer`, { answer: 'extend' })}>Still using it (+7 days)</button>
                        <button class="btn" on:click={() => post(`/api/loans/${loan.ID}/overdue-answer`, { answer: 'returned' })}>I returned it</button>
                        <button class="btn" on:click={() => confirm('Mark it as lost? The inventory managers are told.') && post(`/api/loans/${loan.ID}/overdue-answer`, { answer: 'lost' })}>I lost it</button>
                    </div>
                </div>
            {/each}

            {#each toVerify as item (item.ID)}
                <div class="card">
                    <p class="label">Monthly check</p>
                    <h2>Is {item.name} where the system says?</h2>
                    <p class="muted">You look after this tracked item. A quick check each month keeps it from going missing.</p>
                    <div class="actions">
                        <button class="btn" on:click={() => post(`/api/items/${item.ID}/verify`, { answer: 'yes' })}>Yes, it's there</button>
                        <button class="btn" on:click={() => post(`/api/items/${item.ID}/verify`, { answer: 'no' })}>No, can't find it</button>
                    </div>
                </div>
            {/each}

            <section class="card hued" style="--hue: {AREA.equipment}">
                <p class="label">What you have</p>
                {#if openLoans.length === 0}
                    <p class="muted">Nothing borrowed. <a href="/items">Browse equipment</a></p>
                {/if}
                {#each openLoans as loan (loan.ID)}
                    <div class="row">
                        <div>
                            <div>{loan.item_name}{#if loan.quantity_borrowed > 1}<span class="mono"> × {loan.quantity_borrowed}</span>{/if}</div>
                            <div class="muted small">
                                {#if loan.status === 'pending'}Waiting for approval{:else}Due <span class="mono">{loan.expected_return_date}</span>{/if}
                            </div>
                        </div>
                        <button class="btn" on:click={() => post(`/api/return/${loan.ID}`)}>{loan.status === 'pending' ? 'Cancel request' : 'Returned'}</button>
                    </div>
                {/each}
            </section>

            {#if upcoming.length}
                <section class="card hued" style="--hue: {AREA.mech}">
                    <p class="label">Your bookings</p>
                    {#each upcoming as b (b.ID)}
                        <div class="row">
                            <div>
                                <div class="when"><span class="dot" style={b.thread_id ? `--hue: ${hueFor(b.thread_id)}` : undefined}></span><span class="mono">{when(b)}</span></div>
                                <div class="muted small">{b.purpose}{b.status === 'pending' ? ' · awaiting approval' : ''}</div>
                            </div>
                            {#if new Date(b.start_time) > new Date()}
                                <button class="btn" on:click={() => confirm('Cancel this booking?') && post(`/api/bookings/${b.ID}/cancel`)}>Cancel</button>
                            {/if}
                        </div>
                    {/each}
                </section>
            {/if}

            <a class="card hued link" href="/threads" style="--hue: {AREA.threads}">
                <span class="icon-badge"><Icon name="users" /></span>
                <div class="grow">
                    <p class="label">Research threads</p>
                    <span>Projects you belong to</span>
                </div>
                <Icon name="arrow" />
            </a>

            <details class="card">
                <summary>Change password</summary>
                <form on:submit|preventDefault={changePassword}>
                    <label>
                        Current password
                        <input type="password" bind:value={pw.old_password} required autocomplete="current-password" />
                    </label>
                    <label>
                        New password
                        <input type="password" bind:value={pw.new_password} required minlength="8" maxlength="72" autocomplete="new-password" />
                    </label>
                    <label>
                        Confirm new password
                        <input type="password" bind:value={pw.confirm} required minlength="8" maxlength="72" autocomplete="new-password" />
                    </label>
                    {#if pwMessage}<p class={pwError ? 'error' : 'success'} role="status">{pwMessage}</p>{/if}
                    <button class="btn primary" type="submit" disabled={busy}>{busy ? 'Saving…' : 'Change password'}</button>
                </form>
            </details>
        </div>
    {/if}
</div>

<style>
    .who {
        display: flex;
        align-items: center;
        gap: 14px;
        min-width: 0;
    }

    .avatar {
        display: grid;
        place-items: center;
        flex: none;
        width: 48px;
        height: 48px;
        border-radius: 50%;
        background: var(--accent);
        color: var(--accent-ink);
        font-weight: 650;
        font-size: 1.2rem;
    }

    h2 {
        margin: 0 0 6px;
        font-size: 1rem;
        font-weight: 600;
    }

    .card p {
        margin-top: 0;
    }

    .overdue {
        border-color: var(--warn);
        background: color-mix(in srgb, var(--warn) 8%, var(--card));
    }

    .overdue .label {
        color: var(--warn);
    }

    .actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
    }

    .small {
        font-size: 0.85rem;
    }

    a:not(.card) {
        color: var(--accent);
    }

    .link {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 12px;
        color: inherit;
        text-decoration: none;
    }

    .link:hover {
        border-color: var(--hue);
    }

    .grow {
        flex: 1;
    }

    .when {
        display: flex;
        align-items: center;
        gap: 8px;
    }

    summary {
        cursor: pointer;
        font-weight: 600;
    }

    form {
        margin-top: 16px;
    }

    label {
        display: block;
        margin-bottom: 12px;
        font-size: 0.9rem;
        color: var(--muted);
    }

    input {
        display: block;
        width: 100%;
        box-sizing: border-box;
        margin-top: 6px;
        padding: 10px 12px;
    }

    .error {
        color: var(--danger);
    }

    .success {
        color: var(--ctp-green);
    }
</style>
