<script>
    import { onMount } from 'svelte';
    import { api, clearSession, loginUrl, refreshUser } from '$lib/auth.js';
    import { hueFor } from '$lib/palette.js';

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
    let loaded = false;
    let robotsLoaded = false;
    let robots = {};
    let showPw = false;

    // Plain-English names for the permission keys the server sends
    const PERMS = {
        borrow_items: ['📦', 'Borrow equipment'],
        book_mocap: ['🎥', 'Book the Motion Capture Lab'],
        book_mech: ['🦾', 'Book Mech Lab robots'],
        approve_mech_bookings: ['✅', 'Approve robot bookings and thread access'],
        approve_loans: ['✋', 'Approve loans of restricted items'],
        auto_approve: ['⚡', 'Own bookings and loans need no sign-off'],
        create_threads: ['🧪', 'Create research threads'],
        upload_prints: ['🖨️', 'Send prints to the 3D printers'],
        manage_inventory: ['🧰', 'Manage the inventory'],
        manage_printers: ['🖨️', 'Manage the printers'],
        manage_bookings: ['📅', 'Manage bookings and robots'],
        manage_users: ['🛡️', 'Manage user accounts']
    };

    $: initial = (user?.name || '?').trim().charAt(0).toUpperCase();
    $: openLoans = loans.filter((l) => l.status === 'active' || l.status === 'pending');
    $: missing = loans.filter((l) => l.status === 'not_found');
    $: returned = loans.filter((l) => l.status === 'returned');
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
        const [l, b, c, r] = await Promise.all([
            getJSON('/api/loans/mine'),
            getJSON('/api/bookings/mine'),
            getJSON('/api/items/custodian'),
            robotsLoaded ? null : getJSON('/api/resources')
        ]);
        loans = l || [];
        bookings = b || [];
        custodianItems = c || [];
        if (r) {
            robots = Object.fromEntries(r.map((x) => [x.id, x]));
            robotsLoaded = true;
        }
        loaded = true;
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

    const shortDate = (d) => (d ? new Date(d).toLocaleDateString(undefined, { day: 'numeric', month: 'short' }) : '');
    const when = (b) => `${new Date(b.start_time).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}, ` +
        `${new Date(b.start_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} - ` +
        `${new Date(b.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`;
</script>

<svelte:head><title>My account · RRC</title></svelte:head>

<div class="container">
    {#if user}
        <div class="page-header">
            <div class="who">
                <span class="avatar" aria-hidden="true">{initial}</span>
                <div class="who-text">
                    <h1>👤 {user.name}</h1>
                    <p class="subtitle">{user.group || 'No group'} · {user.email}{#if user.phone}{' · '}<span class="mono">{user.phone}</span>{/if}</p>
                </div>
            </div>
            <button class="danger-btn" on:click={logout}>🚪 Log out</button>
        </div>

        {#if notice}<p class="message {noticeError ? 'error' : 'success'}" role="status">{notice}</p>{/if}

        <nav class="quick-links" aria-label="Shortcuts">
            <a class="quick" style="--tile-accent: var(--rp-iris)" href="/dashboard"><span aria-hidden="true">📊</span> Dashboard</a>
            <a class="quick" style="--tile-accent: var(--rp-foam)" href="/items"><span aria-hidden="true">🧰</span> What's in the Lab</a>
            <a class="quick" style="--tile-accent: var(--rp-love)" href="/mechlab"><span aria-hidden="true">🦾</span> Book a robot</a>
            <a class="quick" style="--tile-accent: var(--rp-rose)" href="/threads"><span aria-hidden="true">🧪</span> Research threads</a>
        </nav>

        {#if overdue.length || toVerify.length}
            <div class="attention">
                {#each overdue as loan (loan.ID)}
                    <div class="panel alert">
                        <p class="panel-label">⏰ Overdue</p>
                        <h3>{loan.item_name} is {loan.days_overdue ? `${loan.days_overdue} day${loan.days_overdue === 1 ? '' : 's'} ` : ''}overdue</h3>
                        <p class="muted">What happened? Your answer keeps the records right{loan.days_overdue >= 7 ? ' and lifts the pause on borrowing and booking' : ''}.</p>
                        <div class="actions">
                            <button class="ghost-btn" on:click={() => post(`/api/loans/${loan.ID}/overdue-answer`, { answer: 'extend' })}>Still using it (+7 days)</button>
                            <button class="ghost-btn" on:click={() => post(`/api/loans/${loan.ID}/overdue-answer`, { answer: 'returned' })}>I returned it</button>
                            <button class="ghost-btn" on:click={() => confirm('Mark it as lost? The inventory managers are told.') && post(`/api/loans/${loan.ID}/overdue-answer`, { answer: 'lost' })}>I lost it</button>
                        </div>
                    </div>
                {/each}

                {#each toVerify as item (item.ID)}
                    <div class="panel check">
                        <p class="panel-label">🔒 Monthly check</p>
                        <h3>Is {item.name} where the system says?</h3>
                        <p class="muted">You look after this tracked item. A quick check each month keeps it from going missing.{#if item.last_verified_at} Last confirmed {shortDate(item.last_verified_at)}.{/if}</p>
                        <div class="actions">
                            <button class="submit-btn" on:click={() => post(`/api/items/${item.ID}/verify`, { answer: 'yes' })}>Yes, it's there</button>
                            <button class="ghost-btn" on:click={() => post(`/api/items/${item.ID}/verify`, { answer: 'no' })}>No, can't find it</button>
                        </div>
                    </div>
                {/each}
            </div>
        {/if}

        <div class="columns">
            <section class="panel">
                <h3 class="section-title" style="--hue: var(--rp-foam)">📦 What you have <span class="count">{openLoans.length}</span></h3>
                {#if !loaded}
                    <p class="muted">Loading…</p>
                {:else if openLoans.length === 0 && missing.length === 0}
                    <p class="empty">Nothing borrowed. <a href="/items">Browse equipment</a></p>
                {/if}
                {#each openLoans as loan (loan.ID)}
                    <div class="list-row">
                        <div class="grow">
                            <div class="item-name">{loan.item_name}{#if loan.quantity_borrowed > 1}<span class="mono">{` × ${loan.quantity_borrowed}`}</span>{/if}</div>
                            <div class="sub">
                                {#if loan.status === 'pending'}
                                    <span class="status-badge warn">Waiting for approval</span>
                                {:else if loan.overdue}
                                    <span class="status-badge overdue">Overdue</span> was due <span class="mono">{loan.expected_return_date}</span>
                                {:else}
                                    Due <span class="mono">{loan.expected_return_date}</span>
                                {/if}
                                {#if loan.lab_location}{' · '}{loan.lab_location}{/if}
                            </div>
                        </div>
                        <button class="ghost-btn" on:click={() => (loan.status === 'pending' || confirm(`Mark ${loan.item_name} as returned?`)) && post(`/api/return/${loan.ID}`)}>{loan.status === 'pending' ? 'Cancel request' : '↩️ Returned'}</button>
                    </div>
                {/each}
                {#each missing as loan (loan.ID)}
                    <div class="list-row">
                        <div class="grow">
                            <div class="item-name">{loan.item_name}</div>
                            <div class="sub"><span class="status-badge overdue">Marked missing</span> An inventory manager will sort it out with you.</div>
                        </div>
                    </div>
                {/each}
                {#if returned.length}
                    <details class="history">
                        <summary>Returned in the last 30 days <span class="mono">({returned.length})</span></summary>
                        {#each returned as loan (loan.ID)}
                            <div class="list-row small">
                                <span>{loan.item_name}{#if loan.quantity_borrowed > 1}<span class="mono">{` × ${loan.quantity_borrowed}`}</span>{/if}</span>
                                <span class="mono muted">{shortDate(loan.returned_at)}</span>
                            </div>
                        {/each}
                    </details>
                {/if}
            </section>

            <section class="panel">
                <h3 class="section-title" style="--hue: var(--rp-gold)">📅 Your bookings <span class="count">{upcoming.length}</span></h3>
                {#if loaded && upcoming.length === 0}
                    <p class="empty">No upcoming bookings. <a href="/mechlab">Book a robot</a> or <a href="/mocap">the MoCap lab</a>.</p>
                {/if}
                {#each upcoming as b (b.ID)}
                    <div class="list-row">
                        <div class="grow">
                            <div class="item-name when">
                                <span class="dot" style="--hue: {b.thread_id ? hueFor(b.thread_id) : 'var(--faint)'}"></span>
                                {robots[b.resource_id]?.name || 'Booking'}
                                {#if b.status === 'pending'}<span class="status-badge warn">Awaiting approval</span>{/if}
                            </div>
                            <div class="sub"><span class="mono">{when(b)}</span>{#if b.thread_name || b.purpose}{' · '}{b.thread_name || b.purpose}{/if}</div>
                        </div>
                        {#if new Date(b.start_time) > new Date()}
                            <button class="ghost-btn" on:click={() => confirm('Cancel this booking?') && post(`/api/bookings/${b.ID}/cancel`)}>Cancel</button>
                        {:else}
                            <span class="status-badge ok">On now</span>
                        {/if}
                    </div>
                {/each}
            </section>

            <section class="panel">
                <h3 class="section-title" style="--hue: var(--rp-iris)">🔑 Your access</h3>
                <p class="muted access-lead">You're in the <strong class="group">{user.group || 'no'}</strong> group{user.status && user.status !== 'active' ? ` (${user.status})` : ''}. This is what your account can do:</p>
                {#if user.permissions?.length}
                    <ul class="perms">
                        {#each user.permissions as perm}
                            <li><span aria-hidden="true">{PERMS[perm]?.[0] || '•'}</span> {PERMS[perm]?.[1] || perm}</li>
                        {/each}
                    </ul>
                {:else}
                    <p class="empty">No permissions yet. Ask a server admin.</p>
                {/if}
                <p class="hint">Need something else? A server admin can change your group or permissions.</p>
            </section>

            <section class="panel">
                <h3 class="section-title" style="--hue: var(--rp-rose)">🔐 Change password</h3>
                <form on:submit|preventDefault={changePassword}>
                    <div class="form-group">
                        <label for="pw-old">Current password</label>
                        <input id="pw-old" type={showPw ? 'text' : 'password'} bind:value={pw.old_password} required autocomplete="current-password" />
                    </div>
                    <div class="form-group">
                        <label for="pw-new">New password <span class="hint-inline">at least 8 characters</span></label>
                        <input id="pw-new" type={showPw ? 'text' : 'password'} bind:value={pw.new_password} required minlength="8" maxlength="72" autocomplete="new-password" />
                    </div>
                    <div class="form-group">
                        <label for="pw-confirm">Confirm new password</label>
                        <input id="pw-confirm" type={showPw ? 'text' : 'password'} bind:value={pw.confirm} required minlength="8" maxlength="72" autocomplete="new-password" />
                        {#if pw.confirm && pw.confirm !== pw.new_password}<span class="field-note bad">The new passwords don't match yet</span>{/if}
                    </div>
                    <label class="check-row"><input type="checkbox" bind:checked={showPw} /> Show passwords</label>
                    {#if pwMessage}<p class="form-msg" class:bad={pwError} role="status">{pwMessage}</p>{/if}
                    <button class="submit-btn wide" type="submit" disabled={busy}>{busy ? 'Saving…' : 'Change password'}</button>
                    <p class="hint">Changing it logs you out everywhere else.</p>
                </form>
            </section>
        </div>
    {:else}
        <p class="empty center">Loading your account…</p>
    {/if}
</div>

<style>
    .container {
        max-width: var(--page-max);
        margin: 0 auto;
        padding: clamp(10px, 2vw, 20px) var(--gutter) 40px;
        box-sizing: border-box;
    }

    .page-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
        border-bottom: 1px solid var(--ctp-surface0);
        padding-bottom: 14px;
        margin-bottom: 18px;
    }

    .who {
        display: flex;
        align-items: center;
        gap: 14px;
        min-width: 0;
    }

    .who-text {
        min-width: 0;
    }

    .avatar {
        display: grid;
        place-items: center;
        flex: none;
        width: 52px;
        height: 52px;
        border-radius: 50%;
        background: linear-gradient(135deg, var(--rp-iris), var(--rp-rose));
        color: var(--accent-ink);
        font-weight: 700;
        font-size: 1.3rem;
        box-shadow: var(--shadow-sm);
    }

    h1 {
        margin: 0;
        font-size: clamp(1.3rem, 3vw, 1.8rem);
        font-weight: 600;
        color: var(--rp-iris);
        overflow-wrap: anywhere;
    }

    .subtitle {
        margin: 2px 0 0;
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
        overflow-wrap: anywhere;
    }

    .quick-links {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 200px), 1fr));
        gap: 10px;
        margin-bottom: 16px;
    }

    .quick {
        position: relative;
        display: flex;
        align-items: center;
        gap: 10px;
        min-height: 48px;
        padding: 10px 14px;
        border: 1px solid var(--ctp-surface0);
        border-radius: 12px;
        background: var(--card);
        color: var(--tile-accent);
        font-weight: 600;
        text-decoration: none;
        overflow: hidden;
        box-sizing: border-box;
        transition: transform var(--normal) var(--ease), border-color var(--normal) var(--ease);
    }

    .quick::after {
        content: '';
        position: absolute;
        left: 0;
        top: 0;
        bottom: 0;
        width: 3px;
        background: var(--tile-accent);
        transform: scaleY(0.3);
        transition: transform var(--normal) var(--ease);
    }

    .quick:hover {
        transform: translateY(-2px);
        border-color: color-mix(in srgb, var(--tile-accent) 55%, var(--ctp-surface1));
    }

    .quick:hover::after {
        transform: scaleY(1);
    }

    :root[data-theme='light'] .quick {
        color: color-mix(in srgb, var(--tile-accent) 80%, var(--rp-text));
    }

    .attention {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr));
        gap: 14px;
        margin-bottom: 16px;
    }

    .columns {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 16px;
        align-items: start;
    }

    .panel {
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--rp-text) 4%, transparent), transparent 60%),
            var(--card);
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        padding: clamp(15px, 3vw, 22px);
        box-shadow: var(--shadow-sm);
        min-width: 0;
    }

    .panel.alert {
        border-color: color-mix(in srgb, var(--warn) 55%, transparent);
        background: color-mix(in srgb, var(--warn) 8%, var(--card));
    }

    .panel.check {
        border-color: color-mix(in srgb, var(--rp-iris) 45%, transparent);
    }

    .panel-label {
        margin: 0 0 6px;
        font-size: 0.8rem;
        font-weight: 700;
        text-transform: uppercase;
        letter-spacing: 0.04em;
        color: var(--ctp-subtext0);
    }

    .panel h3:not(.section-title) {
        margin: 0 0 6px;
        font-size: 1.05rem;
        color: var(--ctp-text);
    }

    .panel p {
        margin-top: 0;
    }

    .section-title {
        margin: 0 0 12px;
        font-size: 1.1rem;
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
        vertical-align: 2px;
    }

    .list-row {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 10px;
        flex-wrap: wrap;
        padding: 12px 0;
        border-top: 1px solid var(--ctp-surface0);
    }

    .section-title + .list-row {
        border-top: none;
        padding-top: 4px;
    }

    .list-row.small {
        padding: 8px 0;
        font-size: 0.9rem;
    }

    .grow {
        flex: 1;
        min-width: 0;
    }

    .item-name {
        font-weight: 600;
        color: var(--ctp-text);
    }

    .when {
        display: flex;
        align-items: center;
        gap: 8px;
        flex-wrap: wrap;
    }

    .sub {
        margin-top: 3px;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
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
        padding: 2px 9px;
        border-radius: 20px;
        font-size: 0.75rem;
        font-weight: 700;
        white-space: nowrap;
    }

    .status-badge.warn {
        background: color-mix(in srgb, var(--rp-gold) 22%, transparent);
        color: var(--rp-gold);
    }

    .status-badge.overdue {
        background: var(--rp-love);
        color: var(--accent-ink);
    }

    .status-badge.ok {
        background: color-mix(in srgb, var(--rp-foam) 22%, transparent);
        color: var(--rp-foam);
    }

    :root[data-theme='light'] .status-badge.warn {
        color: color-mix(in srgb, var(--rp-gold) 60%, var(--rp-text));
    }

    :root[data-theme='light'] .status-badge.ok {
        color: var(--rp-pine);
    }

    .history {
        margin-top: 10px;
        border-top: 1px solid var(--ctp-surface0);
        padding-top: 10px;
    }

    .history summary {
        cursor: pointer;
        min-height: 32px;
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
    }

    .access-lead {
        margin: 0 0 10px;
    }

    .group {
        color: var(--rp-iris);
    }

    .perms {
        list-style: none;
        margin: 0;
        padding: 0;
        display: flex;
        flex-direction: column;
        gap: 6px;
    }

    .perms li {
        padding: 8px 12px;
        border-radius: 8px;
        background: var(--ctp-surface0);
        font-size: 0.9rem;
    }

    .form-group {
        margin-bottom: 14px;
    }

    .form-group label {
        display: block;
        margin-bottom: 6px;
        font-weight: 600;
        font-size: 0.92rem;
        color: var(--ctp-text);
    }

    .form-group input {
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

    .form-group input:focus {
        border-color: var(--rp-rose);
        outline: none;
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--rp-rose) 25%, transparent);
    }

    .hint-inline {
        font-weight: 400;
        font-size: 0.8rem;
        color: var(--ctp-overlay1);
    }

    .check-row {
        display: flex;
        align-items: center;
        gap: 8px;
        min-height: 40px;
        margin-bottom: 8px;
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
        cursor: pointer;
    }

    .check-row input {
        width: 18px;
        height: 18px;
        accent-color: var(--rp-rose);
    }

    .field-note {
        display: block;
        margin-top: 5px;
        font-size: 0.82rem;
    }

    .bad {
        color: var(--danger);
    }

    .form-msg {
        margin: 0 0 12px;
        padding: 10px 14px;
        border-radius: 8px;
        text-align: center;
        font-weight: 500;
        border: 1px solid color-mix(in srgb, var(--rp-foam) 40%, transparent);
        background: color-mix(in srgb, var(--rp-foam) 12%, transparent);
        color: var(--rp-foam);
    }

    :root[data-theme='light'] .form-msg {
        color: var(--rp-pine);
    }

    .form-msg.bad {
        border-color: color-mix(in srgb, var(--danger) 40%, transparent);
        background: color-mix(in srgb, var(--danger) 12%, transparent);
        color: var(--danger);
    }

    .hint {
        margin: 10px 0 0;
        font-size: 0.82rem;
        color: var(--ctp-overlay1);
    }

    .actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
    }

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
        cursor: pointer;
        transition: transform var(--fast) var(--ease), box-shadow var(--fast) var(--ease), background var(--fast) var(--ease);
    }

    .submit-btn {
        border: none;
        background: linear-gradient(135deg, var(--rp-rose), var(--rp-love));
        color: var(--accent-ink);
    }

    .submit-btn.wide {
        width: 100%;
        min-height: 46px;
        font-size: 1rem;
    }

    .submit-btn:hover {
        transform: translateY(-1px);
        box-shadow: 0 6px 18px color-mix(in srgb, var(--rp-rose) 30%, transparent);
    }

    .submit-btn:disabled {
        background: var(--ctp-surface0);
        color: var(--ctp-subtext0);
        transform: none;
        box-shadow: none;
        cursor: wait;
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

    .danger-btn {
        border: none;
        background: linear-gradient(135deg, var(--rp-love), var(--rp-rose));
        color: var(--accent-ink);
    }

    .danger-btn:hover {
        transform: translateY(-1px);
        box-shadow: 0 4px 15px color-mix(in srgb, var(--rp-love) 30%, transparent);
    }

    .muted {
        color: var(--ctp-subtext0);
    }

    .mono {
        font-family: var(--mono);
        font-variant-numeric: tabular-nums;
    }

    .empty {
        margin: 0;
        color: var(--ctp-subtext0);
    }

    .empty.center {
        text-align: center;
        padding: 40px 0;
    }

    .empty a {
        color: var(--link);
    }

    .message.success {
        background: color-mix(in srgb, var(--rp-foam) 15%, var(--card));
    }

    .message.error {
        background: color-mix(in srgb, var(--rp-love) 15%, var(--card));
    }

    @media (max-width: 860px) {
        .columns {
            grid-template-columns: minmax(0, 1fr);
        }
    }

    @media (max-width: 520px) {
        .danger-btn,
        .list-row .ghost-btn {
            width: 100%;
        }
    }
</style>
