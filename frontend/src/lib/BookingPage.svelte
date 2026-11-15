<script>
    // A week calendar for booking a lab or a robot. Used by /mocap (one
    // resource) and /mechlab (one tab per robot), and embedded in the admin
    // page. Robots are booked for a research thread (see /threads). Who may
    // book, cancel or approve is decided by the server;
    // the page only hides buttons that would be refused.
    import { onMount, tick } from 'svelte';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import Icon from '$lib/Icon.svelte';
    import { AREA, hueFor } from '$lib/palette.js';

    export let lab; // 'mocap' | 'mech'
    export let title;
    export let subtitle = '';
    export let embedded = false; // inside the admin page: no page header

    // Calendar shows this hour range, one row per hour
    const DAY_START = 8;
    const DAY_END = 22;
    const HOURS = Array.from({ length: DAY_END - DAY_START }, (_, i) => DAY_START + i);
    const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    const NO_SHOW_GRACE_MS = 15 * 60 * 1000; // matches the server

    let user = null;
    let resources = [];
    let resource = null;
    let bookings = [];
    let pending = [];
    let threads = [];
    let loading = false;
    let submitting = false;
    let message = '';
    let messageType = '';

    // Monday of the week currently on screen
    let weekStart = startOfWeek(new Date());
    let selectedBooking = null;
    let showForm = false;
    let formEl;
    let bookingsRequest = 0; // newest loadBookings call; older replies are dropped

    let form = {
        purpose: '',
        on_behalf_name: '',
        on_behalf_phone: '',
        thread_id: '',
        date: toDateInput(new Date()),
        start_hour: '10:00',
        end_hour: '12:00'
    };

    $: weekDays = Array.from({ length: 7 }, (_, i) => addDays(weekStart, i));
    // One column per day, with its bookings already positioned. Depends on both
    // weekDays and bookings so the grid re-renders whenever either changes.
    $: columns = weekDays.map((day) => ({ day, blocks: layOutDay(day, bookings) }));
    $: weekLabel = `${weekDays[0].toLocaleDateString(undefined, { day: 'numeric', month: 'short' })} - ` +
                   `${weekDays[6].toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })}`;
    $: labHue = AREA[lab];
    // A booking takes its thread's colour, so a thread reads the same everywhere
    const blockHue = (b) => (b.thread_id ? hueFor(b.thread_id) : labHue);
    $: bookPerm = lab === 'mocap' ? 'book_mocap' : 'book_mech';
    // Threads signed off for this robot; your own, unless booking for someone else
    $: hasAccess = (t) => t.active && t.access.some((a) => a.resource_id === resource?.id && a.status === 'approved');
    $: bookableThreads = threads.filter((t) => t.mine && hasAccess(t));
    $: onBehalf = can(user, 'manage_bookings') && form.on_behalf_name.trim() !== '';
    $: threadChoices = onBehalf ? threads.filter(hasAccess) : bookableThreads;
    // Preselect only when a thread is required; otherwise "None" stays a choice
    $: if (resource?.requires_thread && threadChoices.length === 1) form.thread_id = threadChoices[0].ID;
    $: noThread = user && resource?.requires_thread && bookableThreads.length === 0 && !can(user, 'manage_bookings');
    $: mayBook = user && ((can(user, bookPerm) && (!resource?.requires_thread || bookableThreads.length > 0)) || can(user, 'manage_bookings'));
    $: approvable = pending.filter((b) => b.user_id !== user?.id);
    $: pickedThread = threadChoices.find((t) => String(t.ID) === String(form.thread_id));

    onMount(async () => {
        user = getUser();
        user = await refreshUser();
        await loadResources();
        loadSidePanels();
    });

    // --- date helpers ---
    function startOfWeek(date) {
        const d = new Date(date);
        d.setHours(0, 0, 0, 0);
        // Monday as the first column
        const diff = (d.getDay() + 6) % 7;
        d.setDate(d.getDate() - diff);
        return d;
    }

    function addDays(date, days) {
        const d = new Date(date);
        d.setDate(d.getDate() + days);
        return d;
    }

    function toDateInput(date) {
        const d = new Date(date);
        return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
    }

    function sameDay(a, b) {
        return a.getFullYear() === b.getFullYear() &&
               a.getMonth() === b.getMonth() &&
               a.getDate() === b.getDate();
    }

    function isToday(date) {
        return sameDay(date, new Date());
    }

    function formatTime(date) {
        return new Date(date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }

    function formatWhen(b) {
        return `${new Date(b.start_time).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}, ` +
               `${formatTime(b.start_time)} - ${formatTime(b.end_time)}`;
    }

    // --- data ---
    async function loadResources() {
        try {
            const response = await fetch(`/api/resources?lab=${lab}`);
            if (!response.ok) throw new Error();
            resources = await response.json();
        } catch {
            showMessage('Failed to load', 'error');
            return;
        }
        const wanted = new URLSearchParams(location.search).get('robot');
        selectResource(resources.find((r) => r.slug === wanted) || resources[0] || null);
    }

    function selectResource(r) {
        resource = r;
        form.thread_id = '';
        selectedBooking = null;
        showForm = false;
        if (r) loadBookings();
    }

    async function loadBookings() {
        if (!resource) return;
        const request = ++bookingsRequest;
        loading = true;
        try {
            const from = new Date(weekStart);
            const to = addDays(weekStart, 7);
            const response = await fetch(`/api/bookings?resource=${resource.slug}&from=${from.toISOString()}&to=${to.toISOString()}`);
            const data = response.ok ? await response.json() : null;
            if (request !== bookingsRequest) return;
            if (data) bookings = data;
            else showMessage('Failed to load bookings', 'error');
        } catch {
            if (request === bookingsRequest) showMessage('Failed to load bookings', 'error');
        } finally {
            if (request === bookingsRequest) loading = false;
        }
    }

    // Approvals for the people who handle them, and the caller's threads
    async function loadSidePanels() {
        if (!user) return;
        if (can(user, 'approve_mech_bookings')) {
            const response = await api(`/api/bookings/pending?lab=${lab}`).catch(() => null);
            if (response?.ok) pending = await response.json();
        }
        const response = await api('/api/threads').catch(() => null);
        if (response?.ok) threads = await response.json();
    }

    function allowance(t) {
        const a = t.access.find((x) => x.resource_id === resource.id);
        const used = (a?.used_this_week || 0).toFixed(1);
        return a?.hours_per_week ? `${used} of ${a.hours_per_week} h used this week` : 'no weekly limit';
    }

    async function send(path, data, okMessage) {
        try {
            const response = await api(path, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data || {})
            });
            const result = await response.json().catch(() => ({}));
            if (response.status === 401) {
                location.href = loginUrl();
                return false;
            }
            showMessage(response.ok ? result.message || okMessage : result.error || `Request failed (HTTP ${response.status})`, response.ok ? 'success' : 'error');
            return response.ok;
        } catch {
            showMessage('Network error. Please try again.', 'error');
            return false;
        }
    }

    async function submitBooking() {
        const start = new Date(`${form.date}T${form.start_hour}`);
        const end = new Date(`${form.date}T${form.end_hour}`);
        if (!(end > start)) {
            showMessage('End time must be after the start time', 'error');
            return;
        }
        submitting = true;
        const ok = await send('/api/bookings', {
            resource: resource.slug,
            purpose: form.purpose,
            on_behalf_name: form.on_behalf_name,
            on_behalf_phone: form.on_behalf_phone,
            thread_id: Number(form.thread_id) || 0,
            start_time: start.toISOString(),
            end_time: end.toISOString()
        }, 'Booked');
        submitting = false;
        if (ok) {
            showForm = false;
            form = { ...form, purpose: '', on_behalf_name: '', on_behalf_phone: '' };
            weekStart = startOfWeek(start);
            loadBookings();
            loadSidePanels();
        }
    }

    async function act(path, okMessage) {
        if (await send(path, {}, okMessage)) {
            selectedBooking = null;
            loadBookings();
            loadSidePanels();
        }
    }

    // --- calendar layout ---
    // Bookings for one day, positioned by their start/end time. Anything
    // outside the 08-22 grid is clamped to its edge and tagged early/late.
    function layOutDay(day, allBookings) {
        const midnight = new Date(day);
        midnight.setHours(0, 0, 0, 0);
        const span = DAY_END - DAY_START;
        return allBookings
            .map((b) => ({ ...b, start: new Date(b.start_time), end: new Date(b.end_time) }))
            .filter((b) => sameDay(b.start, day))
            .map((b) => {
                // hours since this day's midnight, so a booking past midnight stays > 24
                const startHour = (b.start - midnight) / 3600000;
                const endHour = (b.end - midnight) / 3600000;
                const from = Math.min(Math.max(startHour, DAY_START), DAY_END - 0.5);
                const to = Math.max(Math.min(endHour, DAY_END), from + 0.5);
                return {
                    ...b,
                    top: ((from - DAY_START) / span) * 100,
                    height: ((to - from) / span) * 100,
                    outside: endHour <= DAY_START ? 'early' : startHour >= DAY_END ? 'late' : ''
                };
            });
    }

    // Why the signed-in user can't book here (only used when !mayBook)
    function cantBookReason() {
        if (noThread) {
            return can(user, 'create_threads')
                ? 'Request access for your thread on the Threads page.'
                : "Robots are booked through research threads (a prof's project). Ask your prof to add you to theirs - see who leads what on the Threads page.";
        }
        return "Your account can't book here yet - ask a lab admin.";
    }

    async function openFormAt(day, hour) {
        if (!user) {
            location.href = loginUrl();
            return;
        }
        if (!mayBook) {
            showMessage(cantBookReason(), 'error');
            return;
        }
        form = {
            ...form,
            date: toDateInput(day),
            start_hour: `${String(hour).padStart(2, '0')}:00`,
            end_hour: `${String(Math.min(hour + 2, DAY_END)).padStart(2, '0')}:00`
        };
        showForm = true;
        selectedBooking = null;
        await tick();
        formEl?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }

    function goToWeek(offset) {
        weekStart = addDays(weekStart, offset * 7);
        loadBookings();
    }

    function goToday() {
        weekStart = startOfWeek(new Date());
        loadBookings();
    }

    const isMine = (b) => user && b.user_id === user.id;
    // Same rules as the server: owners only before it starts, managers always
    const mayCancel = (b) => can(user, 'manage_bookings') ||
        (isMine(b) && !b.no_show && new Date(b.start_time).getTime() > Date.now());
    const HALF_DAY_MS = 12 * 3600 * 1000;
    function mayReportNoShow(b) {
        const now = Date.now();
        const start = new Date(b.start_time).getTime();
        const end = new Date(b.end_time).getTime();
        if (!user || isMine(b) || !can(user, bookPerm) || b.status !== 'approved' || b.no_show ||
            now <= start + NO_SHOW_GRACE_MS || now >= end) return false;
        if (can(user, 'manage_bookings') || can(user, 'approve_mech_bookings')) return true;
        // Server: your own approved booking on this resource within 12 h of it.
        // shortcut: only checks the loaded week, so a neighbour just across the week edge is missed.
        return bookings.some((o) => isMine(o) && o.status === 'approved' &&
            new Date(o.start_time).getTime() < end + HALF_DAY_MS && new Date(o.end_time).getTime() > start - HALF_DAY_MS);
    }

    function showMessage(text, type) {
        message = text;
        messageType = type;
        setTimeout(() => {
            message = '';
            messageType = '';
        }, 6000);
    }
</script>

<div class:page={!embedded} style="--hue: {labHue}">
    {#if !embedded}
        <header class="page-head">
            <div class="title">
                <span class="icon-badge"><Icon name={lab === 'mech' ? 'bot' : 'video'} /></span>
                <div>
                    <h1>{title}</h1>
                    {#if subtitle}<p>{subtitle}</p>{/if}
                </div>
            </div>
            <nav class="segmented" aria-label="Booking pages">
                <a href="/mechlab" style="--hue: {AREA.mech}" class:active={lab === 'mech'} aria-current={lab === 'mech' ? 'page' : undefined}>Mech Lab</a>
                <a href="/mocap" style="--hue: {AREA.mocap}" class:active={lab === 'mocap'} aria-current={lab === 'mocap' ? 'page' : undefined}>MoCap Lab</a>
            </nav>
        </header>
    {:else}
        <h2 class="embedded-title">{title}</h2>
    {/if}

    {#if message}
        <div class="message {messageType}" role="status">{message}</div>
    {/if}

    <div class="stack">
        {#if approvable.length > 0}
            <section class="card">
                <h2 class="label">Waiting for your approval</h2>
                {#each approvable as b (b.ID)}
                    <div class="row">
                        <span><span class="dot" style="--hue: {blockHue(b)}"></span><strong>{b.booked_by}</strong> · {resources.find((r) => r.id === b.resource_id)?.name}{b.thread_name ? ` · ${b.thread_name}` : ''} · <span class="mono">{formatWhen(b)}</span>{b.purpose ? ` · ${b.purpose}` : ''}</span>
                        <span class="actions">
                            <button class="btn primary" on:click={() => act(`/api/bookings/${b.ID}/approve`, 'Approved')}>Approve</button>
                            <button class="btn" on:click={() => act(`/api/bookings/${b.ID}/reject`, 'Declined')}>Decline</button>
                        </span>
                    </div>
                {/each}
            </section>
        {/if}

        {#if resources.length > 1}
            <div class="segmented" role="tablist">
                {#each resources as r (r.id)}
                    <button role="tab" aria-selected={resource?.id === r.id} class:active={resource?.id === r.id} style="--hue: {hueFor(r.id)}" on:click={() => selectResource(r)}>
                        <span class="dot"></span>{r.name}
                    </button>
                {/each}
            </div>
        {/if}

        {#if !resource}
            <p class="note">{lab === 'mech' ? 'No robots have been added yet. A lab admin adds them from the admin page.' : 'Loading...'}</p>
        {:else}
            {#if resource.requires_approval || resource.requires_thread || resource.max_hours || resource.max_future_bookings}
                <p class="rules muted">
                    {#if resource.requires_thread}Booked by research threads with access.{/if}
                    {#if resource.requires_approval}Each booking is approved by a lab admin.{/if}
                    {#if resource.max_hours}Up to <span class="mono">{resource.max_hours} h</span> per booking.{/if}
                    {#if resource.max_future_bookings}At most <span class="mono">{resource.max_future_bookings}</span> upcoming per person.{/if}
                </p>
            {/if}

            {#if noThread}
                <section class="card tint">
                    <h2 class="label">No thread with access</h2>
                    <p class="text">None of your research threads can book {resource.name} yet.
                        {#if can(user, 'create_threads')}
                            Request access for your thread on the <a href="/threads">Threads page</a>.
                        {:else}
                            Robots are booked through research threads (a prof's project). Ask your prof to add you to theirs - see who leads what on the <a href="/threads">Threads page</a>.
                        {/if}
                    </p>
                </section>
            {/if}

            <div class="toolbar">
                <div class="week-nav">
                    <button class="btn icon" on:click={() => goToWeek(-1)} aria-label="Previous week"><span class="flip"><Icon name="arrow" size={16} /></span></button>
                    <button class="btn" on:click={goToday}>Today</button>
                    <button class="btn icon" on:click={() => goToWeek(1)} aria-label="Next week"><Icon name="arrow" size={16} /></button>
                    <span class="mono muted">{weekLabel}</span>
                </div>
                {#if mayBook}
                    <button class="btn" class:primary={!showForm} on:click={() => { showForm = !showForm; selectedBooking = null; }}>
                        {showForm ? 'Close' : 'Book a slot'}
                    </button>
                {:else if !user}
                    <a class="btn primary" href={loginUrl()}>Log in to book</a>
                {/if}
            </div>

            {#if showForm}
                <form class="card form" bind:this={formEl} on:submit|preventDefault={submitBooking}>
                    <h2 class="label">New booking · {resource.name}</h2>
                    <div class="form-row">
                        <label>
                            Date
                            <input type="date" bind:value={form.date} required />
                        </label>
                        <label>
                            From
                            <input type="time" bind:value={form.start_hour} required step="900" />
                        </label>
                        <label>
                            To
                            <input type="time" bind:value={form.end_hour} required step="900" />
                        </label>
                    </div>
                    <label>
                        Purpose
                        <input type="text" bind:value={form.purpose} placeholder="Optional" />
                    </label>
                    {#if can(user, 'manage_bookings')}
                        <div class="form-row">
                            <label>
                                For someone else (optional)
                                <input type="text" bind:value={form.on_behalf_name} placeholder="Their name" />
                            </label>
                            <label>
                                Their phone
                                <input type="tel" bind:value={form.on_behalf_phone} placeholder="Optional" />
                            </label>
                        </div>
                    {/if}
                    {#if resource.requires_thread || threadChoices.length}
                        <label>
                            Research thread
                            <select bind:value={form.thread_id} required={resource.requires_thread}>
                                <option value="">{resource.requires_thread ? 'Pick a thread' : 'None'}</option>
                                {#each threadChoices as t (t.ID)}
                                    <option value={t.ID}>{t.name} — {allowance(t)}</option>
                                {/each}
                            </select>
                            {#if pickedThread}<span class="mono muted hint picked"><span class="dot" style="--hue: {hueFor(pickedThread.ID)}"></span>{allowance(pickedThread)}</span>{/if}
                        </label>
                    {/if}
                    {#if resource.requires_approval && !can(user, 'auto_approve')}
                        <p class="hint muted">A lab admin approves this request. You'll get a notification.</p>
                    {/if}
                    <div class="actions">
                        <button type="submit" class="btn primary" disabled={submitting}>
                            {submitting ? 'Booking...' : resource.requires_approval && !can(user, 'auto_approve') ? 'Request booking' : 'Confirm booking'}
                        </button>
                    </div>
                </form>
            {/if}

            {#if selectedBooking}
                <section class="card details">
                    <h2 class="label">Booking</h2>
                    <h3>{selectedBooking.purpose || 'Booking'}</h3>
                    <dl>
                        <dt>Booked by</dt><dd>{selectedBooking.booked_by}</dd>
                        {#if selectedBooking.thread_name}<dt>Thread</dt><dd>{selectedBooking.thread_name}</dd>{/if}
                        <dt>When</dt><dd class="mono">{formatWhen(selectedBooking)}</dd>
                    </dl>
                    {#if selectedBooking.status === 'pending' || selectedBooking.no_show}
                        <p class="chips">
                            {#if selectedBooking.status === 'pending'}<span class="chip warn">Waiting for approval</span>{/if}
                            {#if selectedBooking.no_show}<span class="chip noshow">Reported as a no-show</span>{/if}
                        </p>
                    {/if}
                    <div class="actions">
                        {#if mayCancel(selectedBooking)}
                            <button class="btn danger" on:click={() => confirm('Cancel this booking?') && act(`/api/bookings/${selectedBooking.ID}/cancel`, 'Booking cancelled')}>Cancel booking</button>
                        {/if}
                        {#if selectedBooking.status === 'pending' && can(user, 'approve_mech_bookings') && !isMine(selectedBooking)}
                            <button class="btn primary" on:click={() => act(`/api/bookings/${selectedBooking.ID}/approve`, 'Approved')}>Approve</button>
                            <button class="btn" on:click={() => act(`/api/bookings/${selectedBooking.ID}/reject`, 'Declined')}>Decline</button>
                        {/if}
                        {#if mayReportNoShow(selectedBooking)}
                            <button class="btn" on:click={() => confirm(`Report that ${selectedBooking.booked_by} didn't turn up? Your name is recorded with the report.`) && act(`/api/bookings/${selectedBooking.ID}/no-show`, 'No-show recorded')}>They didn't turn up</button>
                        {/if}
                        {#if selectedBooking.no_show && can(user, 'manage_bookings')}
                            <button class="btn" on:click={() => act(`/api/admin/bookings/${selectedBooking.ID}/clear-no-show`, 'No-show cleared')}>Clear no-show</button>
                        {/if}
                        <button class="btn" on:click={() => (selectedBooking = null)}>Close</button>
                    </div>
                </section>
            {/if}

            <div class="calendar">
                <div class="calendar-head">
                    <div class="time-gutter"></div>
                    {#each weekDays as day}
                        <div class="day-head" class:today={isToday(day)}>
                            <span class="day-name">{DAY_NAMES[day.getDay()]}</span>
                            <span class="day-number mono">{day.getDate()}</span>
                        </div>
                    {/each}
                </div>

                <div class="calendar-body">
                    <div class="time-gutter">
                        {#each HOURS as hour}
                            <div class="time-label mono">{String(hour).padStart(2, '0')}:00</div>
                        {/each}
                    </div>

                    {#each columns as column}
                        <div class="day-column" class:today={isToday(column.day)}>
                            {#each HOURS as hour}
                                <button
                                    class="hour-cell"
                                    on:click={() => openFormAt(column.day, hour)}
                                    title="Book {String(hour).padStart(2, '0')}:00 on {column.day.toDateString()}"
                                    aria-label="Book {String(hour).padStart(2, '0')}:00 on {column.day.toDateString()}"
                                ></button>
                            {/each}

                            {#each column.blocks as booking (booking.ID)}
                                <button
                                    class="booking-block"
                                    class:pending={booking.status === 'pending'}
                                    class:mine={isMine(booking)}
                                    class:noshow={booking.no_show}
                                    style="top: {booking.top}%; height: {booking.height}%; --hue: {blockHue(booking)};"
                                    on:click={() => { selectedBooking = booking; showForm = false; }}
                                >
                                    <span class="block-time mono">{formatTime(booking.start)} - {formatTime(booking.end)}{booking.outside ? ` (${booking.outside})` : ''}</span>
                                    <span class="block-name">{booking.booked_by}{booking.thread_name ? ` · ${booking.thread_name}` : ''}</span>
                                    {#if booking.status === 'pending'}<span class="block-status">Pending</span>{/if}
                                    <span class="block-purpose">{booking.purpose}</span>
                                </button>
                            {/each}
                        </div>
                    {/each}
                </div>
            </div>

            {#if loading}
                <p class="note">Loading bookings...</p>
            {:else if bookings.length === 0}
                <p class="note">No bookings this week.</p>
            {/if}
        {/if}
    </div>
</div>

<style>
    .stack {
        display: flex;
        flex-direction: column;
        gap: 14px;
    }

    .embedded-title {
        margin: 8px 0 14px;
        font-size: 1.1rem;
        font-weight: 600;
    }

    h2.label {
        margin-bottom: 6px;
    }

    .row {
        flex-wrap: wrap;
    }

    .title {
        display: flex;
        align-items: center;
        gap: 12px;
    }

    .row .dot {
        margin-right: 8px;
        vertical-align: middle;
    }

    .text {
        margin: 0;
        line-height: 1.5;
    }

    .text a {
        color: var(--accent);
    }

    .actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
    }

    /* Robot picker: one pill group, the chosen robot tinted */
    .segmented {
        align-self: flex-start;
        display: inline-flex;
        flex-wrap: wrap;
        gap: 2px;
        padding: 3px;
        background: var(--card);
        border: 1px solid var(--line);
        border-radius: var(--radius-pill);
    }

    .segmented button,
    .segmented a {
        display: inline-flex;
        align-items: center;
        min-height: 36px;
        text-decoration: none;
        padding: 0 16px;
        border: none;
        border-radius: var(--radius-pill);
        background: none;
        color: var(--muted);
        font: inherit;
        font-weight: 500;
        cursor: pointer;
    }

    .segmented button:hover,
    .segmented a:hover {
        color: var(--ctp-text);
    }

    .segmented button {
        gap: 8px;
    }

    .segmented button.active,
    .segmented a.active {
        background: color-mix(in srgb, var(--hue) 14%, transparent);
        color: var(--hue);
        font-weight: 600;
    }

    .rules {
        margin: 0;
        font-size: 0.9rem;
    }

    .toolbar {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
    }

    .week-nav {
        display: flex;
        align-items: center;
        gap: 8px;
        flex-wrap: wrap;
    }

    .week-nav .mono {
        margin-left: 6px;
        font-size: 0.9rem;
    }

    .btn.icon {
        width: 40px;
        padding: 0;
    }

    .flip {
        display: inline-flex;
        transform: scaleX(-1);
    }

    /* --- form and details --- */
    .form {
        display: flex;
        flex-direction: column;
        gap: 12px;
    }

    .form-row {
        display: flex;
        gap: 12px;
        flex-wrap: wrap;
    }

    .form label {
        display: flex;
        flex-direction: column;
        gap: 6px;
        flex: 1;
        min-width: 140px;
        font-size: 0.85rem;
        color: var(--muted);
    }

    .form input,
    .form select {
        min-height: 40px;
        box-sizing: border-box;
        padding: 8px 10px;
    }

    .hint {
        margin: 0;
        font-size: 0.8rem;
    }

    .picked {
        display: flex;
        align-items: center;
        gap: 6px;
    }

    .details h3 {
        margin: 0 0 12px;
        font-size: 1.15rem;
        font-weight: 600;
    }

    .details dl {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: 6px 16px;
        margin: 0 0 12px;
        font-size: 0.9rem;
    }

    .details dt {
        color: var(--muted);
    }

    .details dd {
        margin: 0;
    }

    .chips {
        display: flex;
        gap: 6px;
        margin: 0 0 12px;
    }

    .chip.noshow {
        color: var(--danger);
        border-color: color-mix(in srgb, var(--danger) 35%, transparent);
    }

    /* --- calendar --- */
    .calendar {
        background: var(--card);
        border: 1px solid var(--line);
        border-radius: var(--radius-lg);
        overflow: hidden;
    }

    .calendar-head,
    .calendar-body {
        display: grid;
        grid-template-columns: 56px repeat(7, 1fr);
    }

    .calendar-head {
        border-bottom: 1px solid var(--line);
    }

    .day-head {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 2px;
        padding: 10px 2px;
        border-left: 1px solid var(--line);
    }

    .day-name {
        font-size: 0.75rem;
        color: var(--muted);
    }

    .day-number {
        font-size: 1.05rem;
        font-weight: 600;
    }

    .day-head.today {
        background: color-mix(in srgb, var(--hue) 12%, transparent);
    }

    .day-head.today .day-name,
    .day-head.today .day-number {
        color: var(--hue);
    }

    .calendar-body {
        position: relative;
        max-height: 60vh;
        overflow-y: auto;
    }

    .time-label {
        height: 48px;
        padding-right: 8px;
        text-align: right;
        font-size: 0.7rem;
        color: var(--faint);
        transform: translateY(-6px);
    }

    .day-column {
        position: relative;
        border-left: 1px solid var(--line);
    }

    .day-column.today {
        background: color-mix(in srgb, var(--hue) 5%, transparent);
    }

    .hour-cell {
        display: block;
        width: 100%;
        height: 48px;
        padding: 0;
        border: none;
        border-bottom: 1px solid var(--line);
        background: transparent;
        cursor: pointer;
        transition: background-color var(--fast) var(--ease);
    }

    .hour-cell:hover {
        background: var(--line);
    }

    .booking-block {
        position: absolute;
        left: 3px;
        right: 3px;
        display: flex;
        flex-direction: column;
        gap: 1px;
        box-sizing: border-box;
        padding: 4px 6px;
        overflow: hidden;
        border: none;
        border-left: 3px solid var(--hue);
        border-radius: 6px;
        background: color-mix(in srgb, var(--hue) 16%, var(--card));
        color: var(--ctp-text);
        font: inherit;
        font-size: 0.7rem;
        line-height: 1.2;
        text-align: left;
        cursor: pointer;
    }

    .booking-block:hover {
        z-index: 2;
        background: color-mix(in srgb, var(--hue) 22%, var(--card));
    }

    .booking-block.mine {
        border: 1px solid var(--hue);
        border-left-width: 3px;
        background: color-mix(in srgb, var(--hue) 24%, var(--card));
    }

    .booking-block.mine:hover {
        background: color-mix(in srgb, var(--hue) 30%, var(--card));
    }

    /* A request nobody has approved yet does not hold the slot */
    .booking-block.pending {
        border: 1px dashed color-mix(in srgb, var(--hue) 50%, transparent);
        border-left-width: 3px;
        background: color-mix(in srgb, var(--hue) 7%, var(--card));
        color: var(--muted);
    }

    .booking-block.noshow {
        border-left-color: var(--danger);
    }

    .block-time {
        font-weight: 600;
    }

    .block-name {
        font-weight: 550;
    }

    .block-status {
        color: var(--warn);
    }

    .block-purpose {
        color: var(--muted);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .note {
        margin: 0;
        text-align: center;
        font-size: 0.85rem;
        color: var(--faint);
    }

    @media (max-width: 700px) {
        .calendar-head,
        .calendar-body {
            grid-template-columns: 40px repeat(7, minmax(70px, 1fr));
        }

        .calendar {
            overflow-x: auto;
        }

        .block-purpose {
            display: none;
        }
    }
</style>
