<script>
    // One glanceable page per person: what needs them, their next booking and
    // gear, their research threads, and (for lab admins) how the lab is used.
    import { onMount } from 'svelte';
    import { api, getUser, loginUrl, refreshUser } from '$lib/auth.js';
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

    // Links come from the server, but only same-site paths are followed: a
    // single leading "/" (not "//" or "/\\", which browsers read as another site).
    const safeLink = (link) => (typeof link === 'string' && /^\/(?![\/\\])/.test(link) ? link : null);

    // Each "needs you" item takes the colour and emoji of the area it links to.
    function area(link) {
        link = safeLink(link) || '';
        if (link.startsWith('/mechlab')) return [AREA.mech, '🦾'];
        if (link.startsWith('/mocap')) return [AREA.mocap, '🎥'];
        if (link.startsWith('/items') || link.startsWith('/account')) return [AREA.equipment, '📦'];
        if (link.startsWith('/threads')) return [AREA.threads, '🧪'];
        if (link.startsWith('/admin')) return [AREA.admin, '🛡️'];
        if (link.startsWith('/printers')) return [AREA.printers, '🖨️'];
        return [AREA.home, '📌'];
    }

    const overdueText = (loan) => `${loan.days_overdue || ''} ${loan.days_overdue === 1 ? 'day' : 'days'} overdue`.trim();
</script>


<svelte:head><title>Dashboard · RRC</title></svelte:head>

<div class="container">
    <div class="page-header">
        <div class="hello">
            <span class="avatar" aria-hidden="true">{initial}</span>
            <div>
                <h1>📊 {greeting}{firstName ? `, ${firstName}` : ''}</h1>
                <p class="subtitle">{today}</p>
            </div>
        </div>
        <span class="clock mono" aria-hidden="true">{clock}</span>
    </div>

    {#if error}
        <div class="panel error-state" role="alert">
            <p>⚠️ {error}</p>
            <button class="ghost-btn" on:click={() => { error = ''; load(); }}>Try again</button>
        </div>
    {/if}

    <nav class="quick-actions" aria-label="Quick actions">
        <a class="quick" style="--tile-accent: var(--rp-foam)" href="/?view=return"><span aria-hidden="true">↩️</span> Return</a>
        <a class="quick" style="--tile-accent: var(--rp-rose)" href="/?view=borrow"><span aria-hidden="true">📦</span> Borrow</a>
        <a class="quick" style="--tile-accent: var(--rp-love)" href="/mechlab"><span aria-hidden="true">🦾</span> Book a robot</a>
        <a class="quick" style="--tile-accent: var(--rp-iris)" href="/mocap"><span aria-hidden="true">🎥</span> MoCap lab</a>
    </nav>

    {#if !data && !error}
        <p class="note">Loading…</p>
    {:else if data}
        <div class="overview-cards">
            <section class="card stat-card" style="--hue: var(--rp-iris)">
                <h3>🔔 Needs you</h3>
                {#if data.needs_you.length}
                    <div class="stat-number mono">{data.needs_you.length}</div>
                    <p class="card-note">{data.needs_you.length === 1 ? 'thing to sort out' : 'things to sort out'} - see below</p>
                {:else}
                    <div class="stat-word">All clear ✨</div>
                    <p class="card-note">Nothing is waiting on you</p>
                {/if}
            </section>

            <section class="card stat-card" style="--hue: {nextHue}">
                <h3>📅 Next booking</h3>
                {#if next}
                    {#if left}
                        <div class="digits">
                            {#each countdown as [value, unit]}
                                <span><b class="mono">{pad(value)}</b><small>{unit}</small></span>
                            {/each}
                        </div>
                    {:else}
                        <div class="stat-word">Now</div>
                    {/if}
                    <p class="card-line">
                        <strong>{robots[next.resource_id]?.name || 'Robot'}</strong>
                        {#if next.status === 'pending'}<span class="status-badge warn">Pending</span>{/if}
                    </p>
                    <p class="card-note">
                        <span class="mono">{day(next.start_time)}, {time(next.start_time)}–{time(next.end_time)}</span>
                        {#if next.thread_name || next.purpose}<br />{next.thread_name || next.purpose}{/if}
                    </p>
                {:else}
                    <div class="stat-word">Nothing booked</div>
                    <a class="card-btn" href="/mechlab">Book a robot</a>
                {/if}
            </section>

            <section class="card stat-card" style="--hue: var(--rp-foam)">
                <h3>📦 You have</h3>
                {#if openLoans.length}
                    <div class="stat-number mono">{openLoans.length}</div>
                    <div class="mini-list">
                        {#each openLoans as loan (loan.ID)}
                            <div class="mini-row">
                                <span class="truncate">{loan.item_name}{#if loan.quantity_borrowed > 1}{` × ${loan.quantity_borrowed}`}{/if}</span>
                                {#if loan.status === 'not_found'}
                                    <span class="status-badge overdue">Missing</span>
                                {:else if loan.overdue}
                                    <span class="status-badge overdue">{overdueText(loan)}</span>
                                {:else if loan.status === 'pending'}
                                    <span class="status-badge warn">Waiting</span>
                                {:else}
                                    <span class="mono muted nowrap">{loan.expected_return_date}</span>
                                {/if}
                            </div>
                        {/each}
                    </div>
                    <a class="card-link" href="/account">Manage in your account →</a>
                {:else}
                    <div class="stat-word">Nothing borrowed</div>
                    <a class="card-btn" href="/items">See what's in the lab</a>
                {/if}
            </section>
        </div>

        {#if data.needs_you.length}
            <section class="panel">
                <h3 class="section-title" style="--hue: var(--rp-iris)">🔔 Needs you <span class="count">{data.needs_you.length}</span></h3>
                <div class="needs">
                    {#each data.needs_you as item}
                        {@const [hue, emoji] = area(item.link)}
                        {@const href = safeLink(item.link)}
                        {#if href}
                            <a class="need" {href} style="--hue: {hue}">
                                <span class="need-icon" aria-hidden="true">{emoji}</span>
                                <span class="grow">{item.text}</span>
                                <span class="need-arrow" aria-hidden="true">→</span>
                            </a>
                        {:else}
                            <div class="need" style="--hue: {hue}">
                                <span class="need-icon" aria-hidden="true">{emoji}</span>
                                <span class="grow">{item.text}</span>
                            </div>
                        {/if}
                    {/each}
                </div>
            </section>
        {/if}

        <h2 class="group-title">🧪 Your research threads</h2>
        {#if !data.threads.length}
            <a class="panel join" href="/threads">
                <span class="grow">
                    <strong>{user?.permissions?.includes('create_threads') ? 'Start a research thread' : 'Join a research thread to book robots'}</strong>
                    <span class="card-note">{user?.permissions?.includes('create_threads') ? 'Create one, add your students, and request robot access.' : 'Ask your prof to add you, or see who leads what.'}</span>
                </span>
                <span class="need-arrow" aria-hidden="true">→</span>
            </a>
        {:else}
            <div class="threads">
                {#each data.threads as thread (thread.ID)}
                    <section class="thread-card" style="--hue: {hueFor(thread.ID)}">
                        <div class="head">
                            <h3>{thread.name}</h3>
                            {#if thread.can_manage}<a class="small-link" href="/threads">Manage</a>{/if}
                        </div>
                        <p class="card-note left">
                            Lead {thread.lead_name} · <span class="mono">{thread.members.length}</span>
                            {thread.members.length === 1 ? 'member' : 'members'}
                        </p>

                        {#each thread.access as robot (robot.resource_id)}
                            <div class="robot">
                                <div class="split">
                                    <span class="truncate">🦾 {robot.resource_name}</span>
                                    {#if robot.status === 'pending'}
                                        <span class="status-badge warn">Waiting for sign-off</span>
                                    {:else if robot.hours_per_week}
                                        <span class="mono muted nowrap">{hours(robot.used_this_week)} / {robot.hours_per_week} h</span>
                                    {:else}
                                        <span class="muted nowrap"><span class="mono">{hours(robot.used_this_week)}</span> this week</span>
                                    {/if}
                                </div>
                                {#if robot.status === 'approved' && robot.hours_per_week}
                                    <div class="bar" class:over={robot.used_this_week >= robot.hours_per_week}>
                                        <span style="width: {pct(robot.used_this_week, robot.hours_per_week)}"></span>
                                    </div>
                                {/if}
                            </div>
                        {:else}
                            <p class="card-note left">No robot access yet.</p>
                        {/each}

                        {#if thread.upcoming.length}
                            <p class="sub-title">Coming up</p>
                            {#each thread.upcoming.slice(0, 3) as b (b.ID)}
                                <div class="mini-row">
                                    <span class="truncate">
                                        {robots[b.resource_id]?.name || thread.access.find((a) => a.resource_id === b.resource_id)?.resource_name || 'Robot'}
                                        <span class="muted">· {b.booked_by}</span>
                                    </span>
                                    <span class="mono muted nowrap">{day(b.start_time)} {time(b.start_time)}{#if b.status === 'pending'}{' · pending'}{/if}</span>
                                </div>
                            {/each}
                            {#if thread.upcoming.length > 3}<p class="card-note left">+{thread.upcoming.length - 3} more</p>{/if}
                        {/if}

                        {#if thread.gear.length}
                            <p class="sub-title">Gear held</p>
                            {#each thread.gear as loan (loan.ID)}
                                <div class="mini-row">
                                    <span class="truncate">{loan.item_name} <span class="muted">· {loan.borrower_name}</span></span>
                                    {#if loan.overdue}
                                        <span class="status-badge overdue">{overdueText(loan)}</span>
                                    {:else}
                                        <span class="mono muted nowrap">{loan.expected_return_date}</span>
                                    {/if}
                                </div>
                            {/each}
                        {/if}
                    </section>
                {/each}
            </div>
        {/if}

        {#if data.usage}
            {@const u = data.usage}
            <h2 class="group-title">📈 How the lab is used</h2>
            <div class="usage">
                <section class="panel">
                    <h3 class="section-title" style="--hue: var(--rp-love)">Robot hours, last 4 weeks</h3>
                    {#each u.robot_hours as row, i (row.name)}
                        <div class="hbar" style="--hue: {robotHue(row.name, i)}"><span class="truncate">{row.name}</span><div class="bar"><span style="width: {pct(row.hours, maxOf(u.robot_hours))}"></span></div><span class="mono muted">{hours(row.hours)}</span></div>
                    {:else}<p class="muted">No bookings yet.</p>{/each}
                </section>
                <section class="panel">
                    <h3 class="section-title" style="--hue: var(--rp-rose)">By thread</h3>
                    {#each u.thread_hours as row, i (row.name)}
                        <div class="hbar" style="--hue: {threadHue(row.name, i)}"><span class="truncate">{row.name}</span><div class="bar"><span style="width: {pct(row.hours, maxOf(u.thread_hours))}"></span></div><span class="mono muted">{hours(row.hours)}</span></div>
                    {:else}<p class="muted">No thread bookings yet.</p>{/each}
                </section>
                <section class="panel">
                    <h3 class="section-title" style="--hue: var(--rp-foam)">Most borrowed, 90 days</h3>
                    {#each u.top_items as row (row.name)}
                        <div class="mini-row"><span class="truncate">{row.name}</span><span class="mono muted">{row.loans}</span></div>
                    {:else}<p class="muted">No loans yet.</p>{/each}
                </section>
                <section class="card stat-card" style="--hue: var(--rp-gold)">
                    <h3>🚫 No-shows, last 4 weeks</h3>
                    <div class="stat-number mono">{u.no_shows}</div>
                </section>
            </div>
        {/if}

        <p class="footer-note">Tip: return gear from the home page in one tap with <a href="/?view=return">Return Item</a>.</p>
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
        width: 48px;
        height: 48px;
        border-radius: 50%;
        background: linear-gradient(135deg, var(--rp-iris), var(--rp-rose));
        color: var(--accent-ink);
        font-weight: 700;
        font-size: 1.2rem;
        box-shadow: var(--shadow-sm);
    }

    h1 {
        margin: 0;
        font-size: clamp(1.3rem, 3vw, 1.8rem);
        font-weight: 600;
        color: var(--rp-iris);
    }

    .subtitle {
        margin: 2px 0 0;
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
    }

    .clock {
        font-size: 1.8rem;
        color: var(--ctp-subtext0);
    }

    /* --- quick actions, in the style of the home tiles ------------------- */

    .quick-actions {
        display: grid;
        grid-template-columns: repeat(4, minmax(0, 1fr));
        gap: 10px;
        margin-bottom: 18px;
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

    /* --- overview cards (old admin dashboard) ----------------------------- */

    .overview-cards {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 1fr));
        gap: clamp(12px, 2vw, 18px);
        margin-bottom: 18px;
        align-items: start;
    }

    .card,
    .panel,
    .thread-card {
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--rp-text) 4%, transparent), transparent 60%),
            var(--card);
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        box-shadow: var(--shadow-sm);
        min-width: 0;
        box-sizing: border-box;
    }

    .card {
        padding: clamp(18px, 3vw, 26px);
        text-align: center;
        transition: transform var(--normal) var(--ease), box-shadow var(--normal) var(--ease), border-color var(--normal) var(--ease);
    }

    .card:hover {
        transform: translateY(-3px);
        border-color: color-mix(in srgb, var(--hue) 45%, var(--ctp-surface1));
        box-shadow: var(--shadow);
    }

    .card h3 {
        margin: 0 0 6px;
        font-size: 1.05rem;
        color: var(--ctp-text);
    }

    .stat-number {
        margin: 8px 0;
        font-size: clamp(2.6rem, 7vw, 3.6rem);
        font-weight: 700;
        line-height: 1.1;
        color: var(--hue);
    }

    .stat-word {
        margin: 14px 0 10px;
        font-size: 1.5rem;
        font-weight: 650;
        color: var(--hue);
    }

    :root[data-theme='light'] .stat-number,
    :root[data-theme='light'] .stat-word {
        color: color-mix(in srgb, var(--hue) 80%, var(--rp-text));
    }

    .card-line {
        margin: 6px 0 0;
        display: flex;
        justify-content: center;
        align-items: center;
        gap: 8px;
        flex-wrap: wrap;
    }

    .card-note {
        display: block;
        margin: 4px 0 0;
        font-size: 0.88rem;
        color: var(--ctp-subtext0);
    }

    .card-note.left {
        text-align: left;
    }

    .card-btn {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        min-height: 40px;
        padding: 8px 18px;
        border-radius: 8px;
        background: linear-gradient(135deg, var(--rp-rose), var(--rp-love));
        color: var(--accent-ink);
        font-weight: 600;
        text-decoration: none;
        transition: transform var(--fast) var(--ease), box-shadow var(--fast) var(--ease);
    }

    .card-btn:hover {
        transform: translateY(-1px);
        box-shadow: 0 6px 18px color-mix(in srgb, var(--rp-rose) 30%, transparent);
    }

    .card-link,
    .small-link {
        display: inline-block;
        margin-top: 10px;
        color: var(--link);
        font-size: 0.9rem;
        text-decoration: none;
    }

    .card-link:hover,
    .small-link:hover {
        text-decoration: underline;
    }

    .digits {
        display: flex;
        justify-content: center;
        gap: 8px;
        margin: 10px 0 8px;
    }

    .digits span {
        display: grid;
        justify-items: center;
        gap: 4px;
    }

    .digits b {
        padding: 6px 10px;
        border-radius: 8px;
        background: color-mix(in srgb, var(--hue) 14%, transparent);
        border: 1px solid color-mix(in srgb, var(--hue) 30%, transparent);
        color: var(--hue);
        font-size: 1.9rem;
        font-weight: 600;
    }

    :root[data-theme='light'] .digits b {
        color: color-mix(in srgb, var(--hue) 80%, var(--rp-text));
    }

    .digits small {
        font-size: 0.75rem;
        color: var(--ctp-subtext0);
    }

    .mini-list {
        text-align: left;
    }

    .mini-row {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 10px;
        padding: 8px 0;
        border-top: 1px solid var(--ctp-surface0);
        font-size: 0.92rem;
        text-align: left;
    }

    /* --- panels ------------------------------------------------------------ */

    .panel {
        padding: clamp(15px, 3vw, 22px);
        margin-bottom: 18px;
    }

    .section-title {
        margin: 0 0 12px;
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
        background: var(--rp-love);
        color: var(--accent-ink);
        font-size: 0.75rem;
        vertical-align: 2px;
    }

    .needs {
        display: flex;
        flex-direction: column;
        gap: 8px;
    }

    .need {
        display: flex;
        align-items: center;
        gap: 12px;
        min-height: 48px;
        padding: 10px 14px;
        border-radius: 10px;
        border: 1px solid var(--ctp-surface0);
        border-left: 3px solid var(--hue);
        background: color-mix(in srgb, var(--hue) 6%, var(--card));
        color: var(--ctp-text);
        text-decoration: none;
        box-sizing: border-box;
        transition: transform var(--fast) var(--ease), background var(--fast) var(--ease);
    }

    a.need:hover {
        transform: translateX(3px);
        background: color-mix(in srgb, var(--hue) 12%, var(--card));
    }

    .need-icon {
        font-size: 1.3rem;
        flex: none;
    }

    .need-arrow {
        flex: none;
        color: var(--ctp-overlay1);
        font-size: 1.1rem;
    }

    a:hover .need-arrow {
        color: var(--hue, var(--accent));
    }

    .grow {
        flex: 1;
        min-width: 0;
    }

    .group-title {
        margin: 6px 0 12px;
        font-size: 1.2rem;
        font-weight: 600;
        color: var(--ctp-text);
    }

    .join {
        display: flex;
        align-items: center;
        gap: 12px;
        color: var(--ctp-text);
        text-decoration: none;
        --hue: var(--rp-rose);
    }

    .join:hover {
        border-color: var(--rp-rose);
    }

    /* --- threads ------------------------------------------------------------ */

    .threads {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr));
        gap: clamp(12px, 2vw, 18px);
        align-items: start;
        margin-bottom: 18px;
    }

    .thread-card {
        position: relative;
        overflow: hidden;
        padding: clamp(15px, 3vw, 22px);
        border-color: color-mix(in srgb, var(--hue) 30%, var(--ctp-surface0));
    }

    .thread-card::after {
        content: '';
        position: absolute;
        left: 0;
        top: 0;
        bottom: 0;
        width: 4px;
        background: var(--hue);
    }

    .head,
    .split {
        display: flex;
        align-items: baseline;
        justify-content: space-between;
        gap: 10px;
    }

    .head h3 {
        margin: 0;
        font-size: 1.15rem;
        font-weight: 650;
        color: var(--hue);
    }

    :root[data-theme='light'] .head h3 {
        color: color-mix(in srgb, var(--hue) 75%, var(--rp-text));
    }

    .head .small-link {
        margin: 0;
        min-height: 0;
    }

    .robot {
        margin-top: 12px;
    }

    .bar {
        margin-top: 8px;
        height: 8px;
        border-radius: 999px;
        background: var(--ctp-surface0);
        overflow: hidden;
    }

    .bar > span {
        display: block;
        height: 100%;
        border-radius: inherit;
        background: var(--hue, var(--accent));
    }

    .bar.over > span {
        background: var(--warn);
    }

    .sub-title {
        margin: 16px 0 4px;
        font-size: 0.8rem;
        font-weight: 700;
        text-transform: uppercase;
        letter-spacing: 0.04em;
        color: var(--ctp-overlay1);
    }

    /* --- usage ------------------------------------------------------------- */

    .usage {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 1fr));
        gap: clamp(12px, 2vw, 18px);
        align-items: start;
    }

    .usage .panel {
        margin: 0;
    }

    .hbar {
        display: grid;
        grid-template-columns: minmax(0, 7em) 1fr auto;
        align-items: center;
        gap: 10px;
        margin: 10px 0;
        font-size: 0.92rem;
    }

    .hbar .bar {
        margin: 0;
    }

    .status-badge {
        display: inline-block;
        flex: none;
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

    :root[data-theme='light'] .status-badge.warn {
        color: color-mix(in srgb, var(--rp-gold) 60%, var(--rp-text));
    }

    .error-state {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
        border-color: color-mix(in srgb, var(--danger) 45%, transparent);
        color: var(--danger);
    }

    .error-state p {
        margin: 0;
    }

    .ghost-btn {
        min-height: 40px;
        padding: 8px 16px;
        border: 1px solid var(--ctp-surface1);
        border-radius: 8px;
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        font: inherit;
        cursor: pointer;
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

    .footer-note {
        margin: 24px 0 0;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
        text-align: center;
    }

    .footer-note a {
        color: var(--link);
    }

    @media (max-width: 700px) {
        .quick-actions {
            grid-template-columns: repeat(2, minmax(0, 1fr));
        }

        .clock {
            display: none;
        }
    }
</style>
