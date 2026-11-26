<script>
    // One glanceable page per person: what needs them, their next booking and
    // gear, their research threads, and (for lab admins) how the lab is used.
    import { onMount } from 'svelte';
    import { api, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import { AREA, hueFor } from '$lib/palette.js';
    import IconAlert from '@tabler/icons-svelte-runes/icons/alert-circle';
    import IconReturn from '@tabler/icons-svelte-runes/icons/arrow-back-up';
    import IconBorrow from '@tabler/icons-svelte-runes/icons/package-export';
    import IconRobot from '@tabler/icons-svelte-runes/icons/robot';
    import IconVideo from '@tabler/icons-svelte-runes/icons/video';
    import IconPackage from '@tabler/icons-svelte-runes/icons/package';
    import IconFlask from '@tabler/icons-svelte-runes/icons/flask';
    import IconShield from '@tabler/icons-svelte-runes/icons/shield';
    import IconPrinter from '@tabler/icons-svelte-runes/icons/printer';
    import IconPin from '@tabler/icons-svelte-runes/icons/pin';
    import IconBell from '@tabler/icons-svelte-runes/icons/bell';
    import IconSparkles from '@tabler/icons-svelte-runes/icons/sparkles';
    import IconCalendar from '@tabler/icons-svelte-runes/icons/calendar';
    import IconArrowRight from '@tabler/icons-svelte-runes/icons/arrow-right';
    import IconChevronRight from '@tabler/icons-svelte-runes/icons/chevron-right';
    import IconChart from '@tabler/icons-svelte-runes/icons/chart-bar';
    import IconBan from '@tabler/icons-svelte-runes/icons/ban';

    let user = null;
    let data = null;
    let loans = [];
    let bookings = [];
    let robots = {};
    let error = '';
    let now = new Date();

    const json = (response) => (response.ok ? response.json() : []);

    onMount(() => {
        const tick = setInterval(() => (now = new Date()), 30000);
        load();
        return () => clearInterval(tick);
    });

    async function load() {
        user = getUser();
        user = await refreshUser();
        if (!user) {
            location.href = loginUrl();
            return;
        }
        fetch('/api/resources')
            .then(json)
            .then((list) => (robots = Object.fromEntries(list.map((r) => [r.id, r]))))
            .catch(() => {});
        api('/api/loans/mine').then(json).then((list) => (loans = list || [])).catch(() => {});
        api('/api/bookings/mine').then(json).then((list) => (bookings = list || [])).catch(() => {});
        try {
            const response = await api('/api/dashboard');
            if (response.ok) data = await response.json();
            else error = 'Could not load the dashboard';
        } catch {
            error = 'Could not reach the server';
        }
    }

    $: firstName = (user?.name || '').trim().split(/\s+/)[0];
    $: initial = (firstName || '?').charAt(0).toUpperCase();
    $: greeting = now.getHours() < 12 ? 'Good morning' : now.getHours() < 17 ? 'Good afternoon' : 'Good evening';
    $: today = now.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' });
    $: clock = `${pad(now.getHours())}:${pad(now.getMinutes())}`;

    $: openLoans = loans.filter((l) => ['active', 'pending', 'not_found'].includes(l.status));
    $: next = bookings
        .filter((b) => ['approved', 'pending'].includes(b.status) && new Date(b.end_time) > now)
        .sort((a, b) => new Date(a.start_time) - new Date(b.start_time))[0];
    $: left = next ? Math.max(0, new Date(next.start_time) - now) : 0;
    $: countdown = [
        [Math.floor(left / 86400000), 'days'],
        [Math.floor(left / 3600000) % 24, 'hrs'],
        [Math.floor(left / 60000) % 60, 'min']
    ];

    const pad = (n) => String(n).padStart(2, '0');
    const hours = (h) => `${Math.round(h * 10) / 10} h`;
    const day = (d) => new Date(d).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' });
    const time = (d) => new Date(d).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    const pct = (value, max) => `${Math.min(100, max ? (value / max) * 100 : 0)}%`;
    const maxOf = (rows) => Math.max(0, ...rows.map((r) => r.hours));
    $: nextHue = robots[next?.resource_id]?.lab === 'mocap' ? AREA.mocap : AREA.mech;
    // Usage rows only carry names, so match them back to ids for a stable hue.
    $: robotHue = (name, i) => hueFor(Object.values(robots).find((r) => r.name === name)?.id ?? i);
    $: threadHue = (name, i) => hueFor(data?.threads.find((t) => t.name === name)?.ID ?? i);

    // Links come from the server, but only same-site paths are followed: a
    // single leading "/" (not "//" or "/\\", which browsers read as another site).
    const safeLink = (link) => (typeof link === 'string' && /^\/(?![\/\\])/.test(link) ? link : null);

    // Each "needs you" item takes the colour and icon of the area it links to.
    function area(link) {
        link = safeLink(link) || '';
        if (link.startsWith('/mechlab')) return [AREA.mech, IconRobot];
        if (link.startsWith('/mocap')) return [AREA.mocap, IconVideo];
        if (link.startsWith('/items') || link.startsWith('/account')) return [AREA.equipment, IconPackage];
        if (link.startsWith('/threads')) return [AREA.threads, IconFlask];
        if (link.startsWith('/admin')) return [AREA.admin, IconShield];
        if (link.startsWith('/printers')) return [AREA.printers, IconPrinter];
        return [AREA.home, IconPin];
    }

    const overdueText = (loan) => `${loan.days_overdue || ''} ${loan.days_overdue === 1 ? 'day' : 'days'} overdue`.trim();
</script>

<svelte:head><title>Dashboard · RRC</title></svelte:head>

<div class="page-header d-print-none mt-0 mb-3">
    <div class="row g-3 align-items-center">
        <div class="col-auto">
            <span class="avatar avatar-md bg-primary-lt" aria-hidden="true">{initial}</span>
        </div>
        <div class="col min-w-0">
            <div class="page-pretitle">{today}</div>
            <h1 class="page-title">{greeting}{firstName ? `, ${firstName}` : ''}</h1>
        </div>
        <div class="col-auto ms-auto d-none d-sm-block">
            <span class="h2 mb-0 tabular text-secondary" aria-hidden="true">{clock}</span>
        </div>
    </div>
</div>

{#if error}
    <div class="alert alert-danger" role="alert">
        <div class="d-flex align-items-center flex-wrap gap-2">
            <IconAlert size={20} class="icon alert-icon flex-shrink-0" />
            <div class="flex-fill">{error}</div>
            <button class="btn btn-sm btn-outline-danger" type="button" on:click={() => { error = ''; load(); }}>Try again</button>
        </div>
    </div>
{/if}

<nav class="row g-2 mb-3" aria-label="Quick actions">
    <div class="col-6 col-md-3">
        <a class="card card-link card-sm h-100" href="/?view=return">
            <div class="card-body d-flex align-items-center gap-2">
                <span class="avatar avatar-sm bg-info-lt"><IconReturn size={18} class="icon" /></span>
                <span class="fw-medium">Return</span>
            </div>
        </a>
    </div>
    <div class="col-6 col-md-3">
        <a class="card card-link card-sm h-100" href="/?view=borrow">
            <div class="card-body d-flex align-items-center gap-2">
                <span class="avatar avatar-sm bg-warning-lt"><IconBorrow size={18} class="icon" /></span>
                <span class="fw-medium">Borrow</span>
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
        <a class="card card-link card-sm h-100" href="/mocap">
            <div class="card-body d-flex align-items-center gap-2">
                <span class="avatar avatar-sm bg-primary-lt"><IconVideo size={18} class="icon" /></span>
                <span class="fw-medium">MoCap lab</span>
            </div>
        </a>
    </div>
</nav>

{#if !data && !error}
    <div class="card">
        <div class="card-body text-secondary">Loading…</div>
    </div>
{:else if data}
    <div class="row row-cards g-3 mb-3">
        <div class="col-12 col-md-4">
            <section class="card h-100">
                <div class="card-status-top" style="background: var(--rp-iris)"></div>
                <div class="card-body">
                    <div class="subheader d-flex align-items-center gap-1"><IconBell size={16} class="icon" /> Needs you</div>
                    {#if data.needs_you.length}
                        <div class="h1 mb-1 mt-2 tabular">{data.needs_you.length}</div>
                        <div class="text-secondary">{data.needs_you.length === 1 ? 'thing to sort out' : 'things to sort out'}, see below</div>
                    {:else}
                        <div class="h2 mb-1 mt-2 d-flex align-items-center gap-2">
                            All clear <IconSparkles size={22} class="icon text-success" />
                        </div>
                        <div class="text-secondary">Nothing is waiting on you</div>
                    {/if}
                </div>
            </section>
        </div>

        <div class="col-12 col-md-4">
            <section class="card h-100">
                <div class="card-status-top" style="background: {nextHue}"></div>
                <div class="card-body">
                    <div class="subheader d-flex align-items-center gap-1"><IconCalendar size={16} class="icon" /> Next booking</div>
                    {#if next}
                        {#if left}
                            <div class="d-flex gap-3 mt-2 mb-1">
                                {#each countdown as [value, unit]}
                                    <div>
                                        <span class="h1 mb-0 tabular">{pad(value)}</span>
                                        <span class="small text-secondary ms-1">{unit}</span>
                                    </div>
                                {/each}
                            </div>
                        {:else}
                            <div class="h1 mb-1 mt-2">Now</div>
                        {/if}
                        <div class="d-flex align-items-center flex-wrap gap-2">
                            <strong>{robots[next.resource_id]?.name || 'Robot'}</strong>
                            {#if next.status === 'pending'}<span class="badge bg-warning-lt">Pending</span>{/if}
                        </div>
                        <div class="text-secondary small mt-1">
                            <span class="tabular">{day(next.start_time)}, {time(next.start_time)}–{time(next.end_time)}</span>
                            {#if next.thread_name || next.purpose}<br />{next.thread_name || next.purpose}{/if}
                        </div>
                    {:else}
                        <div class="h2 mb-3 mt-2">Nothing booked</div>
                        <a class="btn btn-outline-primary btn-sm" href="/mechlab">Book a robot</a>
                    {/if}
                </div>
            </section>
        </div>

        <div class="col-12 col-md-4">
            <section class="card h-100">
                <div class="card-status-top" style="background: var(--rp-foam)"></div>
                <div class="card-body">
                    <div class="subheader d-flex align-items-center gap-1"><IconPackage size={16} class="icon" /> You have</div>
                    {#if openLoans.length}
                        <div class="h1 mb-2 mt-2 tabular">{openLoans.length}</div>
                        <div class="list-group list-group-flush mb-2">
                            {#each openLoans as loan (loan.ID)}
                                <div class="list-group-item px-0 py-2 d-flex align-items-center justify-content-between gap-2">
                                    <span class="text-truncate">{loan.item_name}{#if loan.quantity_borrowed > 1}{` × ${loan.quantity_borrowed}`}{/if}</span>
                                    {#if loan.status === 'not_found'}
                                        <span class="badge bg-danger">Missing</span>
                                    {:else if loan.overdue}
                                        <span class="badge bg-danger text-nowrap">{overdueText(loan)}</span>
                                    {:else if loan.status === 'pending'}
                                        <span class="badge bg-warning-lt">Waiting</span>
                                    {:else}
                                        <span class="tabular text-secondary small text-nowrap">{loan.expected_return_date}</span>
                                    {/if}
                                </div>
                            {/each}
                        </div>
                        <a class="d-inline-flex align-items-center gap-1" href="/account">Manage in your account <IconArrowRight size={16} class="icon" /></a>
                    {:else}
                        <div class="h2 mb-3 mt-2">Nothing borrowed</div>
                        <a class="btn btn-outline-primary btn-sm" href="/items">See what's in the lab</a>
                    {/if}
                </div>
            </section>
        </div>
    </div>

    {#if data.needs_you.length}
        <section class="card mb-3">
            <div class="card-header">
                <h3 class="card-title d-flex align-items-center gap-2"><IconBell size={18} class="icon text-primary" /> Needs you</h3>
                <div class="card-actions"><span class="badge bg-primary-lt">{data.needs_you.length}</span></div>
            </div>
            <div class="list-group list-group-flush">
                {#each data.needs_you as item}
                    {@const [hue, icon] = area(item.link)}
                    {@const href = safeLink(item.link)}
                    {#if href}
                        <a class="list-group-item list-group-item-action d-flex align-items-center gap-3" {href}>
                            <span class="avatar avatar-sm need-icon" style="--hue: {hue}"><svelte:component this={icon} size={18} class="icon" /></span>
                            <span class="flex-fill min-w-0">{item.text}</span>
                            <IconChevronRight size={18} class="icon text-secondary flex-shrink-0" />
                        </a>
                    {:else}
                        <div class="list-group-item d-flex align-items-center gap-3">
                            <span class="avatar avatar-sm need-icon" style="--hue: {hue}"><svelte:component this={icon} size={18} class="icon" /></span>
                            <span class="flex-fill min-w-0">{item.text}</span>
                        </div>
                    {/if}
                {/each}
            </div>
        </section>
    {/if}

    <h2 class="h3 mt-4 mb-3 d-flex align-items-center gap-2"><IconFlask size={20} class="icon text-success" /> Your research threads</h2>
    {#if !data.threads.length}
        <a class="card card-link mb-3" href="/threads">
            <div class="card-body d-flex align-items-center gap-3">
                <div class="flex-fill">
                    <div class="fw-bold">{user?.permissions?.includes('create_threads') ? 'Start a research thread' : 'Join a research thread to book robots'}</div>
                    <div class="text-secondary small">
                        {user?.permissions?.includes('create_threads') ? 'Create one, add your students, and request robot access.' : 'Ask your prof to add you, or see who leads what.'}
                    </div>
                </div>
                <IconChevronRight size={20} class="icon text-secondary flex-shrink-0" />
            </div>
        </a>
    {:else}
        <div class="row row-cards g-3 mb-3">
            {#each data.threads as thread (thread.ID)}
                <div class="col-12 col-md-6 col-xl-4">
                    <section class="card h-100">
                        <div class="card-status-top" style="background: {hueFor(thread.ID)}"></div>
                        <div class="card-header">
                            <div class="min-w-0">
                                <h3 class="card-title text-break">{thread.name}</h3>
                                <div class="card-subtitle small text-secondary mt-1">
                                    Lead {thread.lead_name} · <span class="tabular">{thread.members.length}</span>
                                    {thread.members.length === 1 ? 'member' : 'members'}
                                </div>
                            </div>
                            {#if thread.can_manage}
                                <div class="card-actions"><a class="btn btn-sm btn-ghost-primary" href="/threads">Manage</a></div>
                            {/if}
                        </div>
                        <div class="card-body">
                            {#each thread.access as robot (robot.resource_id)}
                                <div class="mb-3">
                                    <div class="d-flex align-items-center justify-content-between gap-2 mb-1">
                                        <span class="text-truncate d-flex align-items-center gap-1">
                                            <IconRobot size={16} class="icon text-secondary flex-shrink-0" />{robot.resource_name}
                                        </span>
                                        {#if robot.status === 'pending'}
                                            <span class="badge bg-warning-lt text-nowrap">Waiting for sign-off</span>
                                        {:else if robot.hours_per_week}
                                            <span class="tabular text-secondary small text-nowrap">{hours(robot.used_this_week)} / {robot.hours_per_week} h</span>
                                        {:else}
                                            <span class="text-secondary small text-nowrap"><span class="tabular">{hours(robot.used_this_week)}</span> this week</span>
                                        {/if}
                                    </div>
                                    {#if robot.status === 'approved' && robot.hours_per_week}
                                        <div class="progress progress-sm">
                                            <div
                                                class="progress-bar"
                                                class:bg-danger={robot.used_this_week >= robot.hours_per_week}
                                                style="width: {pct(robot.used_this_week, robot.hours_per_week)}; {robot.used_this_week >= robot.hours_per_week ? '' : `background: ${hueFor(thread.ID)}`}"
                                                role="progressbar"
                                                aria-label="{robot.resource_name} hours used this week"
                                                aria-valuenow={Math.round(robot.used_this_week * 10) / 10}
                                                aria-valuemin="0"
                                                aria-valuemax={robot.hours_per_week}
                                            ></div>
                                        </div>
                                    {/if}
                                </div>
                            {:else}
                                <p class="text-secondary small">No robot access yet.</p>
                            {/each}

                            {#if thread.upcoming.length}
                                <div class="subheader mt-3 mb-1">Coming up</div>
                                <div class="list-group list-group-flush">
                                    {#each thread.upcoming.slice(0, 3) as b (b.ID)}
                                        <div class="list-group-item px-0 py-2 d-flex justify-content-between gap-2 small">
                                            <span class="text-truncate">
                                                {robots[b.resource_id]?.name || thread.access.find((a) => a.resource_id === b.resource_id)?.resource_name || 'Robot'}
                                                <span class="text-secondary">· {b.booked_by}</span>
                                            </span>
                                            <span class="tabular text-secondary text-nowrap">{day(b.start_time)} {time(b.start_time)}{#if b.status === 'pending'}{' · pending'}{/if}</span>
                                        </div>
                                    {/each}
                                </div>
                                {#if thread.upcoming.length > 3}<div class="text-secondary small">+{thread.upcoming.length - 3} more</div>{/if}
                            {/if}

                            {#if thread.gear.length}
                                <div class="subheader mt-3 mb-1">Gear held</div>
                                <div class="list-group list-group-flush">
                                    {#each thread.gear as loan (loan.ID)}
                                        <div class="list-group-item px-0 py-2 d-flex justify-content-between align-items-center gap-2 small">
                                            <span class="text-truncate">{loan.item_name} <span class="text-secondary">· {loan.borrower_name}</span></span>
                                            {#if loan.overdue}
                                                <span class="badge bg-danger text-nowrap">{overdueText(loan)}</span>
                                            {:else}
                                                <span class="tabular text-secondary text-nowrap">{loan.expected_return_date}</span>
                                            {/if}
                                        </div>
                                    {/each}
                                </div>
                            {/if}
                        </div>
                    </section>
                </div>
            {/each}
        </div>
    {/if}

    {#if data.usage}
        {@const u = data.usage}
        <h2 class="h3 mt-4 mb-3 d-flex align-items-center gap-2"><IconChart size={20} class="icon text-info" /> How the lab is used</h2>
        <div class="row row-cards g-3 mb-3">
            <div class="col-12 col-md-6 col-xl-4">
                <section class="card h-100">
                    <div class="card-header"><h3 class="card-title">Robot hours, last 4 weeks</h3></div>
                    <div class="card-body">
                        {#each u.robot_hours as row, i (row.name)}
                            <div class="mb-2">
                                <div class="d-flex justify-content-between gap-2 small mb-1">
                                    <span class="text-truncate">{row.name}</span>
                                    <span class="tabular text-secondary text-nowrap">{hours(row.hours)}</span>
                                </div>
                                <div class="progress progress-sm">
                                    <div class="progress-bar" style="width: {pct(row.hours, maxOf(u.robot_hours))}; background: {robotHue(row.name, i)}"></div>
                                </div>
                            </div>
                        {:else}
                            <div class="empty py-2"><p class="empty-subtitle text-secondary">No bookings yet.</p></div>
                        {/each}
                    </div>
                </section>
            </div>
            <div class="col-12 col-md-6 col-xl-4">
                <section class="card h-100">
                    <div class="card-header"><h3 class="card-title">By thread</h3></div>
                    <div class="card-body">
                        {#each u.thread_hours as row, i (row.name)}
                            <div class="mb-2">
                                <div class="d-flex justify-content-between gap-2 small mb-1">
                                    <span class="text-truncate">{row.name}</span>
                                    <span class="tabular text-secondary text-nowrap">{hours(row.hours)}</span>
                                </div>
                                <div class="progress progress-sm">
                                    <div class="progress-bar" style="width: {pct(row.hours, maxOf(u.thread_hours))}; background: {threadHue(row.name, i)}"></div>
                                </div>
                            </div>
                        {:else}
                            <div class="empty py-2"><p class="empty-subtitle text-secondary">No thread bookings yet.</p></div>
                        {/each}
                    </div>
                </section>
            </div>
            <div class="col-12 col-md-6 col-xl-4">
                <section class="card h-100">
                    <div class="card-header"><h3 class="card-title">Most borrowed, 90 days</h3></div>
                    {#if u.top_items.length}
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr><th>Item</th><th class="w-1 text-end">Loans</th></tr>
                                </thead>
                                <tbody>
                                    {#each u.top_items as row (row.name)}
                                        <tr><td class="text-break">{row.name}</td><td class="text-end tabular text-secondary">{row.loans}</td></tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                    {:else}
                        <div class="card-body"><div class="empty py-2"><p class="empty-subtitle text-secondary">No loans yet.</p></div></div>
                    {/if}
                </section>
            </div>
            <div class="col-12 col-md-6 col-xl-4">
                <section class="card">
                    <div class="card-status-top" style="background: var(--rp-gold)"></div>
                    <div class="card-body">
                        <div class="subheader d-flex align-items-center gap-1"><IconBan size={16} class="icon" /> No-shows, last 4 weeks</div>
                        <div class="h1 mb-0 mt-2 tabular">{u.no_shows}</div>
                    </div>
                </section>
            </div>
        </div>
    {/if}

    <p class="text-secondary small mt-4">
        Tip: return gear from the home page in one tap with <a href="/?view=return">Return Item</a>.
    </p>
{/if}

<style>
    /* Each "needs you" row takes the hue of the area it links to */
    .need-icon {
        color: var(--hue);
        background: color-mix(in srgb, var(--hue) 16%, transparent);
    }
</style>
