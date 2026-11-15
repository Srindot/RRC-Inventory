<script>
    // One glanceable page per person: what needs them, their next booking and
    // gear, their research threads, and (for lab admins) how the lab is used.
    import { onMount } from 'svelte';
    import { api, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import Icon from '$lib/Icon.svelte';
    import { AREA, hueFor } from '$lib/palette.js';

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

    // Each "needs you" item takes the colour and icon of the area it links to.
    function area(link = '') {
        if (link.startsWith('/mechlab')) return [AREA.mech, 'bot'];
        if (link.startsWith('/mocap')) return [AREA.mocap, 'video'];
        if (link.startsWith('/items') || link.startsWith('/account')) return [AREA.equipment, 'box'];
        if (link.startsWith('/threads')) return [AREA.threads, 'users'];
        if (link.startsWith('/admin')) return [AREA.admin, 'shield'];
        return [AREA.home, 'grid'];
    }

    const overdueText = (loan) => `${loan.days_overdue || ''} ${loan.days_overdue === 1 ? 'day' : 'days'} overdue`.trim();
</script>

<svelte:head><title>Dashboard · RRC</title></svelte:head>

<div class="page">
    <header class="page-head">
        <div class="hello">
            <span class="avatar" aria-hidden="true">{initial}</span>
            <div>
                <h1>{greeting}{firstName ? `, ${firstName}` : ''}</h1>
                <p>{today}</p>
            </div>
        </div>
        <span class="clock mono" aria-hidden="true">{clock}</span>
    </header>

    {#if error}<div class="message error" role="status">{error}</div>{/if}

    {#if !data && !error}
        <p class="muted">Loading…</p>
    {:else if data}
        <div class="grid bento">
            <section class="card filled" style="--hue: {AREA.home}">
                <p class="label">Needs you</p>
                <p class="stat mono" class:word={!data.needs_you.length}>{data.needs_you.length || 'All clear'}</p>
                {#each data.needs_you.slice(0, 2) as item}
                    <a class="accent-link" href={item.link}>{item.text}</a>
                {/each}
            </section>

            <section class="card hued" style="--hue: {nextHue}">
                <p class="label">Next booking</p>
                {#if next}
                    {#if left}
                        <div class="digits">
                            {#each countdown as [value, unit]}
                                <span><b class="mono">{pad(value)}</b><small>{unit}</small></span>
                            {/each}
                        </div>
                    {:else}
                        <p class="stat">Now</p>
                    {/if}
                    <p class="line">
                        {robots[next.resource_id]?.name || 'Robot'}
                        {#if next.status === 'pending'}<span class="chip warn">Pending</span>{/if}
                    </p>
                    <p class="line muted">
                        <span class="mono">{day(next.start_time)}, {time(next.start_time)}–{time(next.end_time)}</span>
                        {#if next.thread_name || next.purpose} · {next.thread_name || next.purpose}{/if}
                    </p>
                {:else}
                    <p class="stat word">Nothing booked</p>
                    <a class="link" href="/mechlab">Book a robot</a>
                {/if}
            </section>

            <section class="card hued" style="--hue: {AREA.equipment}">
                <p class="label">You have</p>
                {#if openLoans.length}
                    <p class="stat mono">{openLoans.length}</p>
                    {#each openLoans as loan (loan.ID)}
                        <div class="row">
                            <span class="truncate">{loan.item_name}{#if loan.quantity_borrowed > 1} ×{loan.quantity_borrowed}{/if}</span>
                            {#if loan.overdue}
                                <span class="mono danger">{overdueText(loan)}</span>
                            {:else if loan.status === 'pending'}
                                <span class="muted">Waiting</span>
                            {:else}
                                <span class="mono muted">{loan.expected_return_date}</span>
                            {/if}
                        </div>
                    {/each}
                {:else}
                    <p class="stat word">Nothing borrowed</p>
                {/if}
            </section>

            {#if data.needs_you.length > 2}
                <section class="card full">
                    <p class="label">Needs you</p>
                    {#each data.needs_you as item}
                        {@const [hue, icon] = area(item.link)}
                        <a class="row" href={item.link} style="--hue: {hue}">
                            <span class="icon-badge"><Icon name={icon} /></span>
                            <span class="grow">{item.text}</span>
                            <Icon name="arrow" />
                        </a>
                    {/each}
                </section>
            {/if}

            {#if !data.threads.length && !user?.permissions?.includes('create_threads')}
                <a class="card row-card" href="/threads">
                    <span><span class="label">Research threads</span>Join a research thread to book robots</span>
                    <Icon name="arrow" />
                </a>
            {/if}

            {#each data.threads as thread (thread.ID)}
                <section class="card hued" style="--hue: {hueFor(thread.ID)}">
                    <div class="head">
                        <p class="label">Research thread</p>
                        {#if thread.can_manage}<a class="link" href="/threads">Manage</a>{/if}
                    </div>
                    <h2>{thread.name}</h2>
                    <p class="line muted">
                        Lead {thread.lead_name} · <span class="mono">{thread.members.length}</span>
                        {thread.members.length === 1 ? 'member' : 'members'}
                    </p>

                    {#each thread.access as robot (robot.resource_id)}
                        <div class="robot">
                            <div class="split">
                                <span class="truncate">{robot.resource_name}</span>
                                {#if robot.status === 'pending'}
                                    <span class="chip warn">Waiting for sign-off</span>
                                {:else if robot.hours_per_week}
                                    <span class="mono muted">{hours(robot.used_this_week)} / {robot.hours_per_week} h</span>
                                {:else}
                                    <span class="muted"><span class="mono">{hours(robot.used_this_week)}</span> this week, no limit</span>
                                {/if}
                            </div>
                            {#if robot.status === 'approved' && robot.hours_per_week}
                                <div class="bar" class:over={robot.used_this_week >= robot.hours_per_week}>
                                    <span style="width: {pct(robot.used_this_week, robot.hours_per_week)}"></span>
                                </div>
                            {/if}
                        </div>
                    {/each}

                    {#if thread.upcoming.length}
                        <p class="label sub">Coming up</p>
                        <div>
                        {#each thread.upcoming.slice(0, 3) as b (b.ID)}
                            <div class="row">
                                <span class="truncate">
                                    <span class="dot"></span>
                                    {robots[b.resource_id]?.name || thread.access.find((a) => a.resource_id === b.resource_id)?.resource_name || 'Robot'}
                                    <span class="muted">· {b.booked_by}</span>
                                </span>
                                <span class="mono muted nowrap">{day(b.start_time)} {time(b.start_time)}{#if b.status === 'pending'} · pending{/if}</span>
                            </div>
                        {/each}
                        </div>
                    {/if}

                    {#if thread.gear.length}
                        <p class="label sub">Gear held</p>
                        <div>
                        {#each thread.gear as loan (loan.ID)}
                            <div class="row">
                                <span class="truncate">{loan.item_name} <span class="muted">· {loan.borrower_name}</span></span>
                                <span class="mono nowrap" class:danger={loan.overdue} class:muted={!loan.overdue}>
                                    {loan.overdue ? overdueText(loan) : loan.expected_return_date}
                                </span>
                            </div>
                        {/each}
                        </div>
                    {/if}
                </section>
            {/each}

            {#if data.usage}
                {@const u = data.usage}
                <section class="card">
                    <p class="label">Robot hours, last 4 weeks</p>
                    {#each u.robot_hours as row, i (row.name)}
                        <div class="hbar" style="--hue: {robotHue(row.name, i)}"><span class="truncate">{row.name}</span><div class="bar"><span style="width: {pct(row.hours, maxOf(u.robot_hours))}"></span></div><span class="mono muted">{hours(row.hours)}</span></div>
                    {:else}<p class="muted">No bookings yet.</p>{/each}
                </section>
                <section class="card">
                    <p class="label">By thread</p>
                    {#each u.thread_hours as row, i (row.name)}
                        <div class="hbar" style="--hue: {threadHue(row.name, i)}"><span class="truncate">{row.name}</span><div class="bar"><span style="width: {pct(row.hours, maxOf(u.thread_hours))}"></span></div><span class="mono muted">{hours(row.hours)}</span></div>
                    {:else}<p class="muted">No thread bookings yet.</p>{/each}
                </section>
                <section class="card">
                    <p class="label">Most borrowed, 90 days</p>
                    {#each u.top_items as row (row.name)}
                        <div class="row" style="--hue: {AREA.equipment}"><span class="truncate"><span class="dot"></span>{row.name}</span><span class="mono muted">{row.loans}</span></div>
                    {:else}<p class="muted">No loans yet.</p>{/each}
                </section>
                <section class="card">
                    <p class="label">No-shows, last 4 weeks</p>
                    <p class="stat mono">{u.no_shows}</p>
                </section>
            {/if}
        </div>

        <p class="footer muted">Return gear from the home page in one tap.</p>
    {/if}
</div>

<style>
    .hello {
        display: flex;
        align-items: center;
        gap: 14px;
        min-width: 0;
    }

    .avatar {
        display: grid;
        place-items: center;
        flex: none;
        width: 44px;
        height: 44px;
        border-radius: 50%;
        background: var(--accent);
        color: var(--accent-ink);
        font-weight: 600;
        font-size: 1.1rem;
    }

    .clock {
        font-size: 1.75rem;
        color: var(--muted);
    }

    .bento {
        grid-template-columns: repeat(3, minmax(0, 1fr));
    }

    .full {
        grid-column: 1 / -1;
    }

    .accent-link {
        display: block;
        margin-top: 10px;
        color: inherit;
        text-decoration: underline;
        text-underline-offset: 3px;
    }

    .digits {
        display: flex;
        gap: 8px;
        margin-bottom: 14px;
    }

    .digits span {
        display: grid;
        justify-items: center;
        gap: 4px;
    }

    .digits b {
        padding: 6px 10px;
        border-radius: var(--radius-sm);
        background: color-mix(in srgb, var(--hue) 14%, transparent);
        border: 1px solid color-mix(in srgb, var(--hue) 30%, transparent);
        color: var(--hue);
        font-size: 1.9rem;
        font-weight: 600;
    }

    .digits small {
        font-size: 0.75rem;
        color: var(--muted);
    }

    .stat.word {
        font-family: var(--font);
        font-size: 1.6rem;
    }

    .line {
        margin: 6px 0 0;
    }

    .stat + .row {
        margin-top: 10px;
    }

    .link {
        display: inline-block;
        margin-top: 10px;
        color: var(--accent);
        text-decoration: none;
    }

    a.row {
        color: inherit;
        text-decoration: none;
        min-height: 24px;
    }

    a.row > :global(svg) {
        flex: none;
        color: var(--faint);
    }

    a.row:hover > :global(svg) {
        color: var(--accent);
    }

    .grow {
        flex: 1;
        min-width: 0;
    }

    .dot {
        margin-right: 8px;
        vertical-align: 1px;
    }

    .row-card {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 12px;
        color: inherit;
        text-decoration: none;
    }

    .row-card .label {
        display: block;
    }

    .head,
    .split {
        display: flex;
        align-items: baseline;
        justify-content: space-between;
        gap: 10px;
    }

    .head .link {
        margin: 0;
    }

    h2 {
        margin: 0;
        font-size: 1.15rem;
        font-weight: 600;
    }

    .robot {
        margin-top: 14px;
    }

    .robot .bar {
        margin-top: 8px;
    }

    .bar.over > span {
        background: var(--warn);
    }

    .label.sub {
        margin: 18px 0 0;
    }

    .hbar {
        display: grid;
        grid-template-columns: minmax(0, 7em) 1fr auto;
        align-items: center;
        gap: 10px;
        margin: 10px 0;
    }

    .truncate {
        min-width: 0;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .nowrap {
        white-space: nowrap;
    }

    .danger {
        color: var(--danger);
    }

    .footer {
        margin: 28px 0 0;
        font-size: 0.85rem;
    }

    @media (max-width: 1100px) {
        .bento {
            grid-template-columns: repeat(2, minmax(0, 1fr));
        }
    }

    @media (max-width: 640px) {
        .bento {
            grid-template-columns: minmax(0, 1fr);
        }

        .clock {
            display: none;
        }
    }
</style>
