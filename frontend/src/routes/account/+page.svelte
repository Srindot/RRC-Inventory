<script>
    import { onMount } from 'svelte';
    import { api, clearSession, loginUrl, refreshUser } from '$lib/auth.js';
    import { hueFor } from '$lib/palette.js';
    import IconPackage from '@tabler/icons-svelte-runes/icons/package';
    import IconVideo from '@tabler/icons-svelte-runes/icons/video';
    import IconRobot from '@tabler/icons-svelte-runes/icons/robot';
    import IconCircleCheck from '@tabler/icons-svelte-runes/icons/circle-check';
    import IconHand from '@tabler/icons-svelte-runes/icons/hand-stop';
    import IconBolt from '@tabler/icons-svelte-runes/icons/bolt';
    import IconFlask from '@tabler/icons-svelte-runes/icons/flask';
    import IconPrinter from '@tabler/icons-svelte-runes/icons/printer';
    import IconTools from '@tabler/icons-svelte-runes/icons/tools';
    import IconCalendar from '@tabler/icons-svelte-runes/icons/calendar';
    import IconShield from '@tabler/icons-svelte-runes/icons/shield';
    import IconPoint from '@tabler/icons-svelte-runes/icons/point';
    import IconLogout from '@tabler/icons-svelte-runes/icons/logout';
    import IconDashboard from '@tabler/icons-svelte-runes/icons/layout-dashboard';
    import IconItems from '@tabler/icons-svelte-runes/icons/list-search';
    import IconAlarm from '@tabler/icons-svelte-runes/icons/alarm';
    import IconLockCheck from '@tabler/icons-svelte-runes/icons/lock-check';
    import IconReturn from '@tabler/icons-svelte-runes/icons/arrow-back-up';
    import IconKey from '@tabler/icons-svelte-runes/icons/key';
    import IconLock from '@tabler/icons-svelte-runes/icons/lock';
    import IconArrowRight from '@tabler/icons-svelte-runes/icons/arrow-right';

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
        borrow_items: [IconPackage, 'Borrow equipment'],
        book_mocap: [IconVideo, 'Book the Motion Capture Lab'],
        book_mech: [IconRobot, 'Book Mech Lab robots'],
        approve_mech_bookings: [IconCircleCheck, 'Approve robot bookings and thread access'],
        approve_loans: [IconHand, 'Approve loans of restricted items'],
        auto_approve: [IconBolt, 'Own bookings and loans need no sign-off'],
        create_threads: [IconFlask, 'Create research threads'],
        upload_prints: [IconPrinter, 'Send prints to the 3D printers'],
        manage_inventory: [IconTools, 'Manage the inventory'],
        manage_printers: [IconPrinter, 'Manage the printers'],
        manage_bookings: [IconCalendar, 'Manage bookings and robots'],
        manage_users: [IconShield, 'Manage user accounts']
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

{#if user}
    <div class="page-header d-print-none mt-0 mb-3">
        <div class="row g-3 align-items-center">
            <div class="col-auto">
                <span class="avatar avatar-lg bg-primary-lt" aria-hidden="true">{initial}</span>
            </div>
            <div class="col min-w-0">
                <div class="page-pretitle">Your account</div>
                <h1 class="page-title text-break">{user.name}</h1>
                <div class="text-secondary small mt-1 text-break">
                    {user.group || 'No group'} · {user.email}{#if user.phone}{' · '}<span class="tabular">{user.phone}</span>{/if}
                </div>
            </div>
            <div class="col-12 col-sm-auto ms-auto">
                <button class="btn btn-outline-danger w-100" type="button" on:click={logout}>
                    <IconLogout size={18} class="icon" />
                    Log out
                </button>
            </div>
        </div>
    </div>

    {#if notice}<div class="toast-msg {noticeError ? 'error' : 'success'}" role="status">{notice}</div>{/if}

    <nav class="row g-2 mb-3" aria-label="Shortcuts">
        <div class="col-6 col-md-3">
            <a class="card card-link card-sm h-100" href="/dashboard">
                <div class="card-body d-flex align-items-center gap-2">
                    <span class="avatar avatar-sm bg-primary-lt"><IconDashboard size={18} class="icon" /></span>
                    <span class="fw-medium">Dashboard</span>
                </div>
            </a>
        </div>
        <div class="col-6 col-md-3">
            <a class="card card-link card-sm h-100" href="/items">
                <div class="card-body d-flex align-items-center gap-2">
                    <span class="avatar avatar-sm bg-info-lt"><IconItems size={18} class="icon" /></span>
                    <span class="fw-medium">What's in the Lab</span>
                </div>
            </a>
        </div>
        <div class="col-6 col-md-3">
            <a class="card card-link card-sm h-100" href="/mechlab">
                <div class="card-body d-flex align-items-center gap-2">
                    <span class="avatar avatar-sm bg-danger-lt"><IconRobot size={18} class="icon" /></span>
                    <span class="fw-medium">Book a robot</span>
                </div>
            </a>
        </div>
        <div class="col-6 col-md-3">
            <a class="card card-link card-sm h-100" href="/threads">
                <div class="card-body d-flex align-items-center gap-2">
                    <span class="avatar avatar-sm bg-warning-lt"><IconFlask size={18} class="icon" /></span>
                    <span class="fw-medium">Research threads</span>
                </div>
            </a>
        </div>
    </nav>

    {#if overdue.length || toVerify.length}
        <div class="row row-cards g-3 mb-3">
            {#each overdue as loan (loan.ID)}
                <div class="col-12 col-lg-6">
                    <div class="card h-100">
                        <div class="card-status-start bg-warning"></div>
                        <div class="card-body">
                            <div class="d-flex align-items-center gap-2 text-warning small fw-bold text-uppercase mb-2">
                                <IconAlarm size={16} class="icon" /> Overdue
                            </div>
                            <h3 class="card-title mb-1">
                                {loan.item_name} is {loan.days_overdue ? `${loan.days_overdue} day${loan.days_overdue === 1 ? '' : 's'} ` : ''}overdue
                            </h3>
                            <p class="text-secondary">
                                What happened? Your answer keeps the records right{loan.days_overdue >= 7 ? ' and lifts the pause on borrowing and booking' : ''}.
                            </p>
                            <div class="btn-list">
                                <button class="btn btn-outline-secondary" type="button" on:click={() => post(`/api/loans/${loan.ID}/overdue-answer`, { answer: 'extend' })}>Still using it (+7 days)</button>
                                <button class="btn btn-outline-secondary" type="button" on:click={() => post(`/api/loans/${loan.ID}/overdue-answer`, { answer: 'returned' })}>I returned it</button>
                                <button
                                    class="btn btn-outline-danger"
                                    type="button"
                                    on:click={() => confirm('Mark it as lost? The inventory managers are told.') && post(`/api/loans/${loan.ID}/overdue-answer`, { answer: 'lost' })}
                                >I lost it</button>
                            </div>
                        </div>
                    </div>
                </div>
            {/each}

            {#each toVerify as item (item.ID)}
                <div class="col-12 col-lg-6">
                    <div class="card h-100">
                        <div class="card-status-start bg-primary"></div>
                        <div class="card-body">
                            <div class="d-flex align-items-center gap-2 text-primary small fw-bold text-uppercase mb-2">
                                <IconLockCheck size={16} class="icon" /> Monthly check
                            </div>
                            <h3 class="card-title mb-1">Is {item.name} where the system says?</h3>
                            <p class="text-secondary">
                                You look after this tracked item. A quick check each month keeps it from going missing.{#if item.last_verified_at} Last confirmed {shortDate(item.last_verified_at)}.{/if}
                            </p>
                            <div class="btn-list">
                                <button class="btn btn-primary" type="button" on:click={() => post(`/api/items/${item.ID}/verify`, { answer: 'yes' })}>Yes, it's there</button>
                                <button class="btn btn-outline-secondary" type="button" on:click={() => post(`/api/items/${item.ID}/verify`, { answer: 'no' })}>No, can't find it</button>
                            </div>
                        </div>
                    </div>
                </div>
            {/each}
        </div>
    {/if}

    <div class="row row-cards g-3">
        <div class="col-12 col-lg-6">
            <section class="card h-100">
                <div class="card-header">
                    <h3 class="card-title d-flex align-items-center gap-2">
                        <IconPackage size={18} class="icon text-info" /> What you have
                    </h3>
                    <div class="card-actions"><span class="badge bg-secondary-lt">{openLoans.length}</span></div>
                </div>
                {#if !loaded}
                    <div class="card-body text-secondary">Loading…</div>
                {:else if openLoans.length === 0 && missing.length === 0}
                    <div class="card-body">
                        <div class="empty py-3">
                            <div class="empty-icon"><IconPackage size={40} class="icon" /></div>
                            <p class="empty-title">Nothing borrowed</p>
                            <div class="empty-action">
                                <a class="btn btn-outline-primary" href="/items">Browse equipment <IconArrowRight size={18} class="icon ms-1" /></a>
                            </div>
                        </div>
                    </div>
                {/if}
                {#if openLoans.length || missing.length}
                    <div class="list-group list-group-flush">
                        {#each openLoans as loan (loan.ID)}
                            <div class="list-group-item">
                                <div class="row g-2 align-items-center">
                                    <div class="col min-w-0">
                                        <div class="fw-medium text-break">
                                            {loan.item_name}{#if loan.quantity_borrowed > 1}<span class="tabular text-secondary">{` × ${loan.quantity_borrowed}`}</span>{/if}
                                        </div>
                                        <div class="small text-secondary mt-1">
                                            {#if loan.status === 'pending'}
                                                <span class="badge bg-warning-lt">Waiting for approval</span>
                                            {:else if loan.overdue}
                                                <span class="badge bg-danger">Overdue</span> was due <span class="tabular">{loan.expected_return_date}</span>
                                            {:else}
                                                Due <span class="tabular">{loan.expected_return_date}</span>
                                            {/if}
                                            {#if loan.lab_location}{' · '}{loan.lab_location}{/if}
                                        </div>
                                    </div>
                                    <div class="col-12 col-sm-auto">
                                        <button
                                            class="btn btn-outline-secondary w-100"
                                            type="button"
                                            on:click={() => (loan.status === 'pending' || confirm(`Mark ${loan.item_name} as returned?`)) && post(`/api/return/${loan.ID}`)}
                                        >
                                            {#if loan.status === 'pending'}
                                                Cancel request
                                            {:else}
                                                <IconReturn size={18} class="icon" /> Returned
                                            {/if}
                                        </button>
                                    </div>
                                </div>
                            </div>
                        {/each}
                        {#each missing as loan (loan.ID)}
                            <div class="list-group-item">
                                <div class="fw-medium text-break">{loan.item_name}</div>
                                <div class="small text-secondary mt-1">
                                    <span class="badge bg-danger">Marked missing</span> An inventory manager will sort it out with you.
                                </div>
                            </div>
                        {/each}
                    </div>
                {/if}
                {#if returned.length}
                    <details class="card-footer">
                        <summary class="text-secondary">Returned in the last 30 days <span class="tabular">({returned.length})</span></summary>
                        <div class="list-group list-group-flush mt-2">
                            {#each returned as loan (loan.ID)}
                                <div class="list-group-item px-0 py-2 d-flex justify-content-between gap-2 small">
                                    <span class="text-break">{loan.item_name}{#if loan.quantity_borrowed > 1}<span class="tabular">{` × ${loan.quantity_borrowed}`}</span>{/if}</span>
                                    <span class="tabular text-secondary text-nowrap">{shortDate(loan.returned_at)}</span>
                                </div>
                            {/each}
                        </div>
                    </details>
                {/if}
            </section>
        </div>

        <div class="col-12 col-lg-6">
            <section class="card h-100">
                <div class="card-header">
                    <h3 class="card-title d-flex align-items-center gap-2">
                        <IconCalendar size={18} class="icon text-warning" /> Your bookings
                    </h3>
                    <div class="card-actions"><span class="badge bg-secondary-lt">{upcoming.length}</span></div>
                </div>
                {#if loaded && upcoming.length === 0}
                    <div class="card-body">
                        <div class="empty py-3">
                            <div class="empty-icon"><IconCalendar size={40} class="icon" /></div>
                            <p class="empty-title">No upcoming bookings</p>
                            <p class="empty-subtitle text-secondary">
                                <a href="/mechlab">Book a robot</a> or <a href="/mocap">the MoCap lab</a>.
                            </p>
                        </div>
                    </div>
                {:else if upcoming.length}
                    <div class="list-group list-group-flush">
                        {#each upcoming as b (b.ID)}
                            <div class="list-group-item">
                                <div class="row g-2 align-items-center">
                                    <div class="col min-w-0">
                                        <div class="fw-medium d-flex align-items-center flex-wrap gap-2">
                                            <span class="status-dot" style="background: {b.thread_id ? hueFor(b.thread_id) : 'var(--tblr-tertiary-color)'}"></span>
                                            {robots[b.resource_id]?.name || 'Booking'}
                                            {#if b.status === 'pending'}<span class="badge bg-warning-lt">Awaiting approval</span>{/if}
                                        </div>
                                        <div class="small text-secondary mt-1">
                                            <span class="tabular">{when(b)}</span>{#if b.thread_name || b.purpose}{' · '}{b.thread_name || b.purpose}{/if}
                                        </div>
                                    </div>
                                    <div class="col-12 col-sm-auto">
                                        {#if new Date(b.start_time) > new Date()}
                                            <button class="btn btn-outline-secondary w-100" type="button" on:click={() => confirm('Cancel this booking?') && post(`/api/bookings/${b.ID}/cancel`)}>Cancel</button>
                                        {:else}
                                            <span class="badge bg-success-lt">On now</span>
                                        {/if}
                                    </div>
                                </div>
                            </div>
                        {/each}
                    </div>
                {/if}
            </section>
        </div>

        <div class="col-12 col-lg-6">
            <section class="card h-100">
                <div class="card-header">
                    <h3 class="card-title d-flex align-items-center gap-2">
                        <IconKey size={18} class="icon text-primary" /> Your access
                    </h3>
                </div>
                <div class="card-body">
                    <p class="text-secondary">
                        You're in the <strong class="text-primary">{user.group || 'no'}</strong> group{user.status && user.status !== 'active' ? ` (${user.status})` : ''}. This is what your account can do:
                    </p>
                    {#if user.permissions?.length}
                        <div class="list-group">
                            {#each user.permissions as perm}
                                <div class="list-group-item d-flex align-items-center gap-2 py-2">
                                    <svelte:component this={PERMS[perm]?.[0] || IconPoint} size={18} class="icon text-secondary flex-shrink-0" />
                                    <span>{PERMS[perm]?.[1] || perm}</span>
                                </div>
                            {/each}
                        </div>
                    {:else}
                        <div class="empty py-3">
                            <p class="empty-title">No permissions yet</p>
                            <p class="empty-subtitle text-secondary">Ask a server admin.</p>
                        </div>
                    {/if}
                </div>
                <div class="card-footer text-secondary small">Need something else? A server admin can change your group or permissions.</div>
            </section>
        </div>

        <div class="col-12 col-lg-6">
            <section class="card h-100">
                <div class="card-header">
                    <h3 class="card-title d-flex align-items-center gap-2">
                        <IconLock size={18} class="icon text-danger" /> Change password
                    </h3>
                </div>
                <div class="card-body">
                    <form on:submit|preventDefault={changePassword}>
                        <div class="mb-3">
                            <label class="form-label" for="pw-old">Current password</label>
                            <input id="pw-old" class="form-control" type={showPw ? 'text' : 'password'} bind:value={pw.old_password} required autocomplete="current-password" />
                        </div>
                        <div class="mb-3">
                            <label class="form-label" for="pw-new">
                                New password <span class="form-label-description">at least 8 characters</span>
                            </label>
                            <input
                                id="pw-new"
                                class="form-control"
                                type={showPw ? 'text' : 'password'}
                                bind:value={pw.new_password}
                                required
                                minlength="8"
                                maxlength="72"
                                autocomplete="new-password"
                            />
                        </div>
                        <div class="mb-3">
                            <label class="form-label" for="pw-confirm">Confirm new password</label>
                            <input
                                id="pw-confirm"
                                class="form-control"
                                class:is-invalid={pw.confirm && pw.confirm !== pw.new_password}
                                type={showPw ? 'text' : 'password'}
                                bind:value={pw.confirm}
                                required
                                minlength="8"
                                maxlength="72"
                                autocomplete="new-password"
                            />
                            {#if pw.confirm && pw.confirm !== pw.new_password}<div class="invalid-feedback">The new passwords don't match yet</div>{/if}
                        </div>
                        <label class="form-check">
                            <input class="form-check-input" type="checkbox" bind:checked={showPw} />
                            <span class="form-check-label">Show passwords</span>
                        </label>
                        {#if pwMessage}
                            <div class="alert {pwError ? 'alert-danger' : 'alert-success'} py-2" role="status">{pwMessage}</div>
                        {/if}
                        <button class="btn btn-primary w-100" type="submit" disabled={busy}>{busy ? 'Saving…' : 'Change password'}</button>
                        <div class="form-hint mt-2">Changing it logs you out everywhere else.</div>
                    </form>
                </div>
            </section>
        </div>
    </div>
{:else}
    <div class="empty">
        <p class="empty-title">Loading your account…</p>
    </div>
{/if}
