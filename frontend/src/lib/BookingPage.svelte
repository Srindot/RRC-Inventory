<script>
    // A week calendar for booking a lab or a robot, in the look of the original
    // Motion Capture Lab page. Used by /mocap (one resource) and /mechlab (one
    // tab per robot), and embedded in the admin page. Robots are booked for a
    // research thread (see /threads). Who may book, cancel or approve is
    // decided by the server; the page only hides buttons that would be refused.
    import { onMount, tick } from 'svelte';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import { AREA, hueFor } from '$lib/palette.js';

    export let lab; // 'mocap' | 'mech'
    export let title;
    export let subtitle = '';
    export let embedded = false; // inside the admin page: no page header

    const EMOJI = { mocap: '🎥', mech: '🦾' };

    // Calendar shows this hour range, one row per hour
    const DAY_START = 8;
    const DAY_END = 22;
    const HOURS = Array.from({ length: DAY_END - DAY_START }, (_, i) => DAY_START + i);
    const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    const NO_SHOW_GRACE_MS = 15 * 60 * 1000; // matches the server

    let user = null;
    let resources = [];
    let resourcesState = 'loading'; // 'loading' | 'ready' | 'error'
    let resource = null;
    let bookings = [];
    let bookingsError = false;
    let pending = [];
    let threads = [];
    let mine = [];
    let loading = false;
    let submitting = false;
    let message = '';
    let messageType = '';
    let messageTimer;

    // Monday of the week currently on screen
    let weekStart = startOfWeek(new Date());
    let selectedBooking = null;
    let showForm = false;
    let formEl;
    let detailsEl;
    let calendarEl;
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
    // Logged in, but the account itself has no booking permission here
    $: noPermission = user && !can(user, bookPerm) && !can(user, 'manage_bookings');
    $: approvable = pending.filter((b) => b.user_id !== user?.id);
    $: pickedThread = threadChoices.find((t) => String(t.ID) === String(form.thread_id));
    $: needsApproval = resource?.requires_approval && !can(user, 'auto_approve');
    // Your own upcoming bookings in this lab (any of its robots)
    // Ticks every minute so past hours and the "now" line stay current
    let now = Date.now();
    // Clashes with what's already on the calendar, shown before submitting
    $: formStart = new Date(`${form.date}T${form.start_hour}`);
    $: formEnd = new Date(`${form.date}T${form.end_hour}`);
    $: clashes = showForm && formEnd > formStart
        ? bookings.filter((b) => new Date(b.start_time) < formEnd && new Date(b.end_time) > formStart)
        : [];
    $: formHours = formEnd > formStart ? (formEnd - formStart) / 3600000 : 0;
    $: tooLong = resource?.max_hours && formHours > resource.max_hours;
    $: durations = [1, 2, 3, 4, 6].filter((h) => !resource?.max_hours || h <= resource.max_hours);
    $: myUpcoming = mine
        .filter((b) => resources.some((r) => r.id === b.resource_id) && new Date(b.end_time).getTime() > Date.now())
        .sort((a, b) => new Date(a.start_time) - new Date(b.start_time));

    onMount(async () => {
        user = getUser();
        user = await refreshUser();
        await loadResources();
        loadSidePanels();
        const timer = setInterval(() => (now = Date.now()), 60000);
        return () => clearInterval(timer);
    });

    // A cell is past once its hour has ended
    const isPast = (day, hour, nowMs) => {
        const end = new Date(day);
        end.setHours(hour + 1, 0, 0, 0);
        return end.getTime() <= nowMs;
    };

    // Position of the "now" line in today's column, or null outside the grid
    function nowTop(nowMs) {
        const d = new Date(nowMs);
        const h = d.getHours() + d.getMinutes() / 60;
        return h < DAY_START || h > DAY_END ? null : ((h - DAY_START) / (DAY_END - DAY_START)) * 100;
    }

    function setDuration(hours) {
        const [h, m] = form.start_hour.split(':').map(Number);
        const total = Math.min(h * 60 + m + hours * 60, 24 * 60 - 1);
        form.end_hour = `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`;
    }

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
        return `${new Date(b.start_time).toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'short' })}, ` +
               `${formatTime(b.start_time)} - ${formatTime(b.end_time)}`;
    }

    const resourceName = (id) => resources.find((r) => r.id === id)?.name || '';

    // --- data ---
    async function loadResources() {
        resourcesState = 'loading';
        try {
            const response = await fetch(`/api/resources?lab=${lab}`);
            if (!response.ok) throw new Error();
            resources = await response.json();
            if (!Array.isArray(resources)) throw new Error();
        } catch {
            resources = [];
            resourcesState = 'error';
            return;
        }
        resourcesState = 'ready';
        const wanted = new URLSearchParams(location.search).get('robot');
        selectResource(resources.find((r) => r.slug === wanted) || resources[0] || null);
        await tick();
        scrollToToday();
    }

    // On a phone the week is wider than the screen: start at today's column
    function scrollToToday() {
        const today = calendarEl?.querySelector('.day-head.today');
        if (!today || calendarEl.scrollWidth <= calendarEl.clientWidth) return;
        calendarEl.scrollLeft = Math.max(0, today.offsetLeft - 40);
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
            bookingsError = !data;
            if (data) bookings = data;
            else showMessage('Failed to load bookings', 'error');
        } catch {
            if (request === bookingsRequest) {
                bookingsError = true;
                showMessage('Failed to load bookings', 'error');
            }
        } finally {
            if (request === bookingsRequest) loading = false;
        }
    }

    // Approvals for the people who handle them, the caller's threads and
    // the caller's own upcoming bookings
    async function loadSidePanels() {
        if (!user) return;
        if (can(user, 'approve_mech_bookings')) {
            const response = await api(`/api/bookings/pending?lab=${lab}`).catch(() => null);
            if (response?.ok) pending = await response.json();
        }
        const [threadsResponse, mineResponse] = await Promise.all([
            api('/api/threads').catch(() => null),
            api('/api/bookings/mine').catch(() => null)
        ]);
        if (threadsResponse?.ok) threads = await threadsResponse.json();
        if (mineResponse?.ok) mine = (await mineResponse.json()) || [];
    }

    function allowance(t) {
        const a = t.access.find((x) => x.resource_id === resource?.id);
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
        }, needsApproval ? 'Requested - a lab admin will approve it' : 'Booked!');
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

    const cancelBooking = (b) =>
        confirm('Cancel this booking?') && act(`/api/bookings/${b.ID}/cancel`, 'Booking cancelled');

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

    async function showBooking(b) {
        selectedBooking = b;
        showForm = false;
        await tick();
        detailsEl?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }

    // From "Your upcoming bookings": go to that robot and week, then open it
    async function jumpTo(b) {
        const r = resources.find((x) => x.id === b.resource_id);
        if (r && r.id !== resource?.id) selectResource(r);
        weekStart = startOfWeek(new Date(b.start_time));
        await loadBookings();
        showBooking(b);
    }

    function goToWeek(offset) {
        weekStart = addDays(weekStart, offset * 7);
        loadBookings();
    }

    async function goToday() {
        weekStart = startOfWeek(new Date());
        loadBookings();
        await tick();
        scrollToToday();
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
        clearTimeout(messageTimer);
        messageTimer = setTimeout(() => {
            message = '';
            messageType = '';
        }, 6000);
    }
</script>

<div class="container" class:embedded style="--hue: {labHue}">
    {#if !embedded}
        <div class="header">
            <div class="logo-title">
                <div>
                    <h1>{EMOJI[lab] || ''} {title}</h1>
                    {#if subtitle}<p class="subtitle">{subtitle}</p>{/if}
                </div>
            </div>
            <nav class="lab-switch" aria-label="Booking pages">
                <a href="/mechlab" style="--hue: {AREA.mech}" class:active={lab === 'mech'} aria-current={lab === 'mech' ? 'page' : undefined}>🦾 Mech Lab</a>
                <a href="/mocap" style="--hue: {AREA.mocap}" class:active={lab === 'mocap'} aria-current={lab === 'mocap' ? 'page' : undefined}>🎥 MoCap Lab</a>
            </nav>
        </div>
    {:else}
        <h2 class="embedded-title">{EMOJI[lab] || ''} {title}</h2>
    {/if}

    {#if message}
        <div class="msg {messageType}" role="status">{message}</div>
    {/if}

    {#if approvable.length > 0}
        <div class="panel approvals">
            <h3>⏳ Waiting for your approval</h3>
            {#each approvable as b (b.ID)}
                <div class="list-row">
                    <span class="list-text">
                        <span class="dot" style="--hue: {blockHue(b)}"></span>
                        <strong>{b.booked_by}</strong> · {resourceName(b.resource_id)}{b.thread_name ? ` · ${b.thread_name}` : ''}
                        <span class="when">{formatWhen(b)}</span>{b.purpose ? ` · ${b.purpose}` : ''}
                    </span>
                    <span class="row-actions">
                        <button class="submit-btn" on:click={() => act(`/api/bookings/${b.ID}/approve`, 'Approved')}>✓ Approve</button>
                        <button class="close-btn" on:click={() => act(`/api/bookings/${b.ID}/reject`, 'Declined')}>✕ Decline</button>
                    </span>
                </div>
            {/each}
        </div>
    {/if}

    {#if resourcesState === 'error'}
        <div class="panel state error-state">
            <h3>⚠️ Couldn't load the booking calendar</h3>
            <p>The server didn't answer. Check your connection and try again.</p>
            <button class="close-btn" on:click={async () => { await loadResources(); loadSidePanels(); }}>↻ Try again</button>
        </div>
    {:else if resourcesState === 'loading'}
        <p class="loading-note">Loading...</p>
    {:else if !resource}
        <div class="panel state">
            <h3>{lab === 'mech' ? '🦾 No robots yet' : '🎥 Not open for booking yet'}</h3>
            <p>
                {lab === 'mech'
                    ? 'No robots have been added yet. A lab admin adds them from the admin page.'
                    : "The Motion Capture Lab hasn't been set up for booking yet. A lab admin adds it from the admin page."}
            </p>
        </div>
    {:else}
        {#if resources.length > 1}
            <div class="robot-tabs" role="tablist" aria-label="Robots">
                {#each resources as r (r.id)}
                    <button role="tab" aria-selected={resource?.id === r.id} class:active={resource?.id === r.id} style="--hue: {hueFor(r.id)}" on:click={() => selectResource(r)}>
                        <span class="dot"></span>{r.name}
                    </button>
                {/each}
            </div>
        {/if}

        {#if resource.requires_approval || resource.requires_thread || resource.max_hours || resource.max_future_bookings}
            <p class="rules">
                {#if resource.requires_thread}<span>🧵 Booked by research threads with access</span>{/if}
                {#if resource.requires_approval}<span>✅ Each booking is approved by a lab admin</span>{/if}
                {#if resource.max_hours}<span>⏱ Up to {resource.max_hours} h per booking</span>{/if}
                {#if resource.max_future_bookings}<span>📅 At most {resource.max_future_bookings} upcoming per person</span>{/if}
            </p>
        {/if}

        {#if !user}
            <div class="panel notice">
                <p>🔒 Anyone can see the calendar. <strong>To book a slot, log in with your lab account</strong> - new here? <a href="/register">Create an account</a>.</p>
                <a class="book-btn" href={loginUrl()}>🔑 Log in to Book</a>
            </div>
        {:else if noPermission}
            <div class="panel notice">
                <p>🚫 Your account can't book {lab === 'mech' ? 'robots' : 'this lab'} yet - ask a lab admin to give your group access.</p>
            </div>
        {:else if noThread}
            <div class="panel notice">
                <p>🧵 None of your research threads can book <strong>{resource.name}</strong> yet.
                    {#if can(user, 'create_threads')}
                        Request access for your thread on the <a href="/threads">Threads page</a>.
                    {:else}
                        Robots are booked through research threads (a prof's project). Ask your prof to add you to theirs - see who leads what on the <a href="/threads">Threads page</a>.
                    {/if}
                </p>
            </div>
        {/if}

        <div class="toolbar">
            <div class="week-nav">
                <button on:click={() => goToWeek(-1)} aria-label="Previous week">◀</button>
                <button class="today-btn" on:click={goToday}>Today</button>
                <button on:click={() => goToWeek(1)} aria-label="Next week">▶</button>
                <span class="week-label">{weekLabel}</span>
            </div>
            {#if mayBook}
                <button class="book-btn" on:click={() => { showForm = !showForm; selectedBooking = null; }}>
                    {showForm ? '✕ Close' : '➕ Book a Slot'}
                </button>
            {/if}
        </div>

        {#if showForm}
            <form class="booking-form" bind:this={formEl} on:submit|preventDefault={submitBooking}>
                <p class="form-title">New booking · <strong>{resource.name}</strong></p>
                <p class="form-note">Booking as <strong>{user?.name || user?.email}</strong>{user?.phone ? ` (${user.phone})` : ''}.</p>
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
                <div class="durations" role="group" aria-label="Length">
                    <span>Length:</span>
                    {#each durations as h}
                        <button type="button" class:active={formHours === h} on:click={() => setDuration(h)}>{h} h</button>
                    {/each}
                </div>
                {#if tooLong}
                    <p class="form-warn">⚠️ {resource.name} can be booked for at most {resource.max_hours} h at a time.</p>
                {/if}
                {#if clashes.length}
                    <p class="form-warn">⚠️ Overlaps {clashes.map((b) => `${b.booked_by} (${formatTime(b.start_time)} - ${formatTime(b.end_time)}${b.status === 'pending' ? ', pending' : ''})`).join(', ')}.</p>
                {/if}
                <label>
                    Purpose
                    <input type="text" bind:value={form.purpose} placeholder="What are you using the {lab === 'mech' ? 'robot' : 'lab'} for? (optional)" />
                </label>
                {#if can(user, 'manage_bookings')}
                    <div class="form-row">
                        <label>
                            Booking for someone else? Their name
                            <input type="text" bind:value={form.on_behalf_name} placeholder="Leave empty to book for yourself" />
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
                        {#if pickedThread}
                            <span class="picked"><span class="dot" style="--hue: {hueFor(pickedThread.ID)}"></span>{pickedThread.name}: {allowance(pickedThread)}</span>
                        {/if}
                    </label>
                {/if}
                {#if needsApproval}
                    <p class="form-note">⏳ A lab admin approves this request. You'll get a notification.</p>
                {/if}
                <button type="submit" class="submit-btn" disabled={submitting}>
                    {submitting ? 'Booking...' : needsApproval ? 'Request Booking' : 'Confirm Booking'}
                </button>
            </form>
        {/if}

        {#if selectedBooking}
            <div class="booking-details" bind:this={detailsEl} style="--hue: {blockHue(selectedBooking)}">
                <h3>{selectedBooking.purpose || 'Booking'}</h3>
                <p><strong>Booked by:</strong> {selectedBooking.booked_by}{isMine(selectedBooking) ? ' (you)' : ''}</p>
                {#if resources.length > 1}<p><strong>Robot:</strong> {resourceName(selectedBooking.resource_id)}</p>{/if}
                {#if selectedBooking.thread_name}
                    <p><strong>Thread:</strong> <span class="dot" style="--hue: {blockHue(selectedBooking)}"></span>{selectedBooking.thread_name}</p>
                {/if}
                <p><strong>When:</strong> {formatWhen(selectedBooking)}</p>
                {#if selectedBooking.status === 'pending' || selectedBooking.no_show}
                    <p class="chips">
                        {#if selectedBooking.status === 'pending'}<span class="chip warn">⏳ Waiting for approval</span>{/if}
                        {#if selectedBooking.no_show}<span class="chip noshow">🚫 Reported as a no-show</span>{/if}
                    </p>
                {/if}
                <div class="details-actions">
                    {#if mayCancel(selectedBooking)}
                        <button class="cancel-btn" on:click={() => cancelBooking(selectedBooking)}>Cancel Booking</button>
                    {/if}
                    {#if selectedBooking.status === 'pending' && can(user, 'approve_mech_bookings') && !isMine(selectedBooking)}
                        <button class="submit-btn" on:click={() => act(`/api/bookings/${selectedBooking.ID}/approve`, 'Approved')}>✓ Approve</button>
                        <button class="close-btn" on:click={() => act(`/api/bookings/${selectedBooking.ID}/reject`, 'Declined')}>✕ Decline</button>
                    {/if}
                    {#if mayReportNoShow(selectedBooking)}
                        <button class="close-btn" on:click={() => confirm(`Report that ${selectedBooking.booked_by} didn't turn up? Your name is recorded with the report.`) && act(`/api/bookings/${selectedBooking.ID}/no-show`, 'No-show recorded')}>They didn't turn up</button>
                    {/if}
                    {#if selectedBooking.no_show && can(user, 'manage_bookings')}
                        <button class="close-btn" on:click={() => act(`/api/admin/bookings/${selectedBooking.ID}/clear-no-show`, 'No-show cleared')}>Clear no-show</button>
                    {/if}
                    {#if !user}
                        <a class="close-btn" href={loginUrl()}>Log in to cancel your booking</a>
                    {/if}
                    <button class="close-btn" on:click={() => (selectedBooking = null)}>Close</button>
                </div>
            </div>
        {/if}

        <div class="calendar" bind:this={calendarEl}>
            <div class="calendar-head">
                <div class="time-gutter"></div>
                {#each weekDays as day}
                    <div class="day-head" class:today={isToday(day)}>
                        <span class="day-name">{DAY_NAMES[day.getDay()]}</span>
                        <span class="day-number">{day.getDate()}</span>
                    </div>
                {/each}
            </div>

            <div class="calendar-body">
                <div class="time-gutter">
                    {#each HOURS as hour}
                        <div class="time-label">{String(hour).padStart(2, '0')}:00</div>
                    {/each}
                </div>

                {#each columns as column}
                    <div class="day-column" class:today={isToday(column.day)}>
                        {#each HOURS as hour}
                            <button
                                class="hour-cell"
                                class:past={isPast(column.day, hour, now)}
                                disabled={isPast(column.day, hour, now)}
                                on:click={() => openFormAt(column.day, hour)}
                                title="{user ? 'Book' : 'Log in to book'} {String(hour).padStart(2, '0')}:00 on {column.day.toDateString()}"
                                aria-label="{user ? 'Book' : 'Log in to book'} {String(hour).padStart(2, '0')}:00 on {column.day.toDateString()}"
                            ></button>
                        {/each}

                        {#if isToday(column.day) && nowTop(now) !== null}
                            <div class="now-line" style="top: {nowTop(now)}%" aria-hidden="true"></div>
                        {/if}
                        {#each column.blocks as booking (booking.ID)}
                            <button
                                class="booking-block"
                                class:pending={booking.status === 'pending'}
                                class:mine={isMine(booking)}
                                class:noshow={booking.no_show}
                                style="top: {booking.top}%; height: {booking.height}%; --hue: {blockHue(booking)};"
                                on:click={() => showBooking(booking)}
                            >
                                <span class="block-time">{formatTime(booking.start)} - {formatTime(booking.end)}{booking.outside ? ` (${booking.outside})` : ''}</span>
                                <span class="block-name">{isMine(booking) ? '★ ' : ''}{booking.booked_by}{booking.thread_name ? ` · ${booking.thread_name}` : ''}</span>
                                {#if booking.status === 'pending'}<span class="block-status">⏳ Pending</span>{/if}
                                {#if booking.no_show}<span class="block-status">🚫 No-show</span>{/if}
                                <span class="block-purpose">{booking.purpose}</span>
                            </button>
                        {/each}
                    </div>
                {/each}
            </div>
        </div>

        {#if loading}
            <p class="loading-note">Loading bookings...</p>
        {:else if bookingsError}
            <p class="loading-note">Couldn't load this week's bookings. <button class="link-btn" on:click={loadBookings}>Try again</button></p>
        {:else if bookings.length === 0}
            <p class="loading-note">No bookings this week - the {lab === 'mech' ? 'robot' : 'lab'} is all yours.</p>
        {/if}

        <p class="legend">
            <span><span class="swatch"></span>Booked</span>
            <span><span class="swatch pending"></span>Pending approval</span>
            {#if user}<span>★ Yours</span>{/if}
            <span><span class="swatch past"></span>Past</span>
            {#if mayBook || !user}<span>Tap a free hour to {user ? 'book it' : 'log in and book'}</span>{/if}
        </p>

        {#if myUpcoming.length > 0}
            <div class="panel mine-panel">
                <h3>📋 Your upcoming bookings</h3>
                {#each myUpcoming as b (b.ID)}
                    <div class="list-row">
                        <button class="list-text link-row" on:click={() => jumpTo(b)} title="Show on the calendar">
                            <span class="dot" style="--hue: {blockHue(b)}"></span>
                            {#if resources.length > 1}<strong>{resourceName(b.resource_id)}</strong> · {/if}
                            <span class="when">{formatWhen(b)}</span>{b.thread_name ? ` · ${b.thread_name}` : ''}{b.purpose ? ` · ${b.purpose}` : ''}
                            {#if b.status === 'pending'}<span class="chip warn">⏳ Pending</span>{/if}
                            {#if b.no_show}<span class="chip noshow">🚫 No-show</span>{/if}
                        </button>
                        {#if mayCancel(b)}
                            <span class="row-actions">
                                <button class="cancel-btn" on:click={() => cancelBooking(b)}>Cancel</button>
                            </span>
                        {/if}
                    </div>
                {/each}
            </div>
        {/if}
    {/if}
</div>

<style>
    .container {
        max-width: var(--page-max);
        margin: 0 auto;
        padding: 16px var(--gutter);
        box-sizing: border-box;
        min-width: 0;
    }

    .container.embedded {
        padding: 0;
    }

    .header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
        border-bottom: 1px solid var(--ctp-surface0);
        padding-bottom: 12px;
    }

    .logo-title {
        display: flex;
        align-items: center;
        gap: 12px;
    }

    h1 {
        margin: 0;
        font-size: clamp(1.2rem, 3vw, 1.6rem);
        color: var(--hue);
    }

    .subtitle {
        margin: 2px 0 0;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
    }

    .embedded-title {
        margin: 8px 0 14px;
        font-size: 1.1rem;
        color: var(--hue);
    }

    /* Lab switcher, where the old page had its back link */
    .lab-switch {
        display: flex;
        gap: 6px;
        flex-wrap: wrap;
    }

    .lab-switch a,
    .robot-tabs button {
        display: inline-flex;
        align-items: center;
        gap: 8px;
        min-height: 40px;
        box-sizing: border-box;
        padding: 0 14px;
        border: none;
        border-radius: 6px;
        background: color-mix(in srgb, var(--ctp-surface0) 60%, transparent);
        color: var(--ctp-text);
        font: inherit;
        font-size: 0.9rem;
        text-decoration: none;
        cursor: pointer;
        transition: background-color var(--fast) var(--ease), filter var(--fast) var(--ease);
    }

    .lab-switch a:hover,
    .robot-tabs button:hover {
        background: color-mix(in srgb, var(--ctp-surface1) 60%, transparent);
    }

    .lab-switch a.active,
    .robot-tabs button.active {
        background: var(--hue);
        color: var(--rp-base);
        font-weight: 650;
    }

    .robot-tabs {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
        margin: 14px 0 0;
    }

    .robot-tabs button.active .dot {
        background: color-mix(in srgb, var(--rp-base) 60%, transparent);
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .dot {
        display: inline-block;
        width: 9px;
        height: 9px;
        flex: none;
        border-radius: 50%;
        background: var(--hue);
        margin-right: 6px;
        vertical-align: middle;
    }

    .robot-tabs .dot {
        margin-right: 0;
    }

    /* Inline like the old page, but it sticks to the top while scrolled so a
       message from a click deep in the calendar is still seen. */
    .msg {
        position: sticky;
        top: 8px;
        z-index: 20;
        margin: 12px 0;
        padding: 10px 14px;
        border-radius: 8px;
        font-size: 0.9rem;
        box-shadow: var(--shadow-sm);
    }

    .msg.success {
        background: color-mix(in srgb, var(--ctp-green) 14%, color-mix(in srgb, var(--card) 60%, transparent));
        color: var(--ctp-green);
        border: 1px solid var(--ctp-green);
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .msg.error {
        background: color-mix(in srgb, var(--ctp-red) 14%, color-mix(in srgb, var(--card) 60%, transparent));
        color: var(--ctp-red);
        border: 1px solid var(--ctp-red);
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .rules {
        display: flex;
        flex-wrap: wrap;
        gap: 6px 16px;
        margin: 12px 0 0;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
    }

    /* Cards: the old form / details card look */
    .panel,
    .booking-form,
    .booking-details {
        background: color-mix(in srgb, var(--ctp-mantle) 60%, transparent);
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        padding: 16px;
        margin: 14px 0 16px;
        box-shadow: var(--shadow-sm);
        animation: rise var(--normal) var(--ease) both;
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .panel h3 {
        margin: 0 0 8px;
        font-size: 1rem;
        color: var(--ctp-mauve);
    }

    .panel p {
        margin: 0;
        font-size: 0.9rem;
        line-height: 1.5;
    }

    .panel a:not(.book-btn) {
        color: var(--link);
    }

    .notice {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 12px;
        flex-wrap: wrap;
        border-color: color-mix(in srgb, var(--hue) 45%, transparent);
        background: color-mix(in srgb, var(--hue) 8%, color-mix(in srgb, var(--ctp-mantle) 60%, transparent));
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .notice p {
        flex: 1;
        min-width: 220px;
    }

    .state {
        text-align: center;
        padding: 28px 16px;
    }

    .state p {
        margin-bottom: 12px;
        color: var(--ctp-subtext0);
    }

    .error-state {
        border-color: var(--ctp-red);
    }

    .error-state h3 {
        color: var(--ctp-red);
    }

    .list-row {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 8px 12px;
        flex-wrap: wrap;
        padding: 8px 0;
        border-top: 1px solid var(--ctp-surface0);
        font-size: 0.9rem;
    }

    .list-row:first-of-type {
        border-top: none;
    }

    .list-text {
        flex: 1;
        min-width: 0;
        line-height: 1.5;
    }

    .link-row {
        min-height: 40px;
        padding: 4px 6px;
        margin: 0 -6px;
        border: none;
        border-radius: 6px;
        background: none;
        color: inherit;
        font: inherit;
        text-align: left;
        cursor: pointer;
    }

    .link-row:hover {
        background: color-mix(in srgb, var(--ctp-surface0) 60%, transparent);
    }

    .when {
        color: var(--ctp-subtext0);
    }

    .row-actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
    }

    .toolbar {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
        margin: 14px 0;
    }

    .week-nav {
        display: flex;
        align-items: center;
        gap: 8px;
        flex-wrap: wrap;
    }

    .week-nav button {
        background: color-mix(in srgb, var(--ctp-surface0) 60%, transparent);
        color: var(--ctp-text);
        border: none;
        border-radius: 6px;
        padding: 8px 12px;
        min-width: 40px;
        min-height: 40px;
        cursor: pointer;
    }

    .week-nav button:hover {
        background: color-mix(in srgb, var(--ctp-surface1) 60%, transparent);
    }

    .week-label {
        color: var(--ctp-subtext0);
        font-size: 0.9rem;
        margin-left: 6px;
    }

    .book-btn {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        box-sizing: border-box;
        background: linear-gradient(135deg, var(--ctp-mauve), var(--ctp-lavender));
        color: var(--accent-ink);
        border: none;
        border-radius: var(--radius-sm);
        padding: 10px 18px;
        font: inherit;
        font-weight: 650;
        text-decoration: none;
        cursor: pointer;
        min-height: 42px;
        box-shadow: var(--shadow-sm);
        transition:
            transform var(--fast) var(--ease),
            box-shadow var(--fast) var(--ease),
            filter var(--fast) var(--ease);
    }

    .book-btn:hover {
        filter: brightness(1.06);
        transform: translateY(-1px);
        box-shadow: var(--shadow);
    }

    .book-btn:active {
        transform: none;
    }

    .form-title {
        margin: 0 0 6px;
        color: var(--ctp-mauve);
        font-size: 1rem;
    }

    .form-row {
        display: flex;
        gap: 12px;
        flex-wrap: wrap;
    }

    .booking-form label {
        display: flex;
        flex-direction: column;
        gap: 4px;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
        flex: 1;
        min-width: 140px;
        margin-bottom: 10px;
    }

    .booking-form input,
    .booking-form select {
        background: color-mix(in srgb, var(--ctp-base) 60%, transparent);
        border: 1px solid var(--ctp-surface1);
        border-radius: 6px;
        padding: 9px;
        min-height: 40px;
        box-sizing: border-box;
        color: var(--ctp-text);
        font-size: 0.95rem;
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .picked {
        display: flex;
        align-items: center;
        font-size: 0.8rem;
        color: var(--ctp-subtext0);
    }

    .form-note {
        font-size: 0.8rem;
        color: var(--ctp-overlay0);
        margin: 0 0 10px;
    }

    .form-note strong {
        color: var(--ctp-text);
    }

    .submit-btn {
        background: var(--ctp-green);
        color: var(--rp-base);
        border: none;
        border-radius: 8px;
        padding: 10px 18px;
        min-height: 40px;
        font: inherit;
        font-weight: 600;
        cursor: pointer;
    }

    .submit-btn:disabled {
        opacity: 0.6;
        cursor: not-allowed;
    }

    .booking-details {
        border-left: 4px solid var(--hue);
    }

    .booking-details h3 {
        margin: 0 0 8px;
        color: var(--ctp-mauve);
    }

    .booking-details p {
        margin: 4px 0;
        font-size: 0.9rem;
    }

    .chips {
        display: flex;
        gap: 6px;
        flex-wrap: wrap;
    }

    .chip {
        display: inline-block;
        margin-left: 6px;
        padding: 2px 8px;
        border-radius: var(--radius-pill);
        border: 1px solid currentColor;
        font-size: 0.75rem;
        white-space: nowrap;
    }

    .chips .chip {
        margin-left: 0;
    }

    .chip.warn {
        color: var(--warn);
    }

    .chip.noshow {
        color: var(--danger);
    }

    .details-actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
        margin-top: 12px;
    }

    .cancel-btn {
        background: var(--ctp-red);
        color: var(--rp-base);
        border: none;
        border-radius: 6px;
        padding: 8px 14px;
        min-height: 40px;
        font: inherit;
        cursor: pointer;
        font-weight: 600;
    }

    .close-btn {
        display: inline-flex;
        align-items: center;
        box-sizing: border-box;
        background: color-mix(in srgb, var(--ctp-surface0) 60%, transparent);
        color: var(--ctp-text);
        border: none;
        border-radius: 6px;
        padding: 8px 14px;
        min-height: 40px;
        font: inherit;
        text-decoration: none;
        cursor: pointer;
    }

    .close-btn:hover {
        background: color-mix(in srgb, var(--ctp-surface1) 60%, transparent);
    }

    .link-btn {
        border: none;
        background: none;
        padding: 8px 4px;
        color: var(--link);
        font: inherit;
        text-decoration: underline;
        cursor: pointer;
    }

    /* --- calendar --- */
    .calendar {
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        overflow: hidden;
        background: color-mix(in srgb, var(--ctp-mantle) 60%, transparent);
        box-shadow: var(--shadow-sm);
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .calendar-head,
    .calendar-body {
        display: grid;
        grid-template-columns: 56px repeat(7, 1fr);
    }

    .calendar-head {
        border-bottom: 1px solid var(--ctp-surface0);
    }

    .day-head {
        text-align: center;
        padding: 8px 2px;
        border-left: 1px solid var(--ctp-surface0);
        display: flex;
        flex-direction: column;
    }

    .day-name {
        font-size: 0.75rem;
        color: var(--ctp-subtext0);
        text-transform: uppercase;
    }

    .day-number {
        font-size: 1.1rem;
        font-weight: 600;
    }

    .day-head.today .day-number {
        color: var(--rp-base);
        background: var(--ctp-mauve);
        border-radius: 50%;
        width: 28px;
        height: 28px;
        line-height: 28px;
        margin: 2px auto 0;
    }

    .calendar-body {
        position: relative;
        max-height: 60vh;
        overflow-y: auto;
    }

    .time-gutter {
        border-right: 1px solid var(--ctp-surface0);
    }

    .time-label {
        height: 48px;
        font-size: 0.7rem;
        color: var(--ctp-overlay0);
        text-align: right;
        padding-right: 6px;
        transform: translateY(-6px);
    }

    /* The first label sits on the top edge; shifting it up would clip it */
    .time-label:first-child {
        transform: none;
    }

    .day-column {
        position: relative;
        border-left: 1px solid var(--ctp-surface0);
    }

    .day-column.today {
        background: color-mix(in srgb, var(--ctp-mauve) 6%, transparent);
    }

    .hour-cell {
        display: block;
        width: 100%;
        height: 48px;
        border: none;
        border-bottom: 1px solid var(--ctp-surface0);
        background: transparent;
        cursor: pointer;
        padding: 0;
        transition: background-color var(--fast) var(--ease);
    }

    .hour-cell:hover {
        background: color-mix(in srgb, var(--ctp-mauve) 16%, transparent);
        box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--ctp-mauve) 35%, transparent);
    }

    .hour-cell.past {
        cursor: default;
        background: repeating-linear-gradient(135deg, transparent 0 6px, var(--line) 6px 7px);
    }

    .hour-cell.past:hover {
        box-shadow: none;
    }

    .now-line {
        position: absolute;
        left: 0;
        right: 0;
        height: 2px;
        background: var(--danger);
        z-index: 3;
        pointer-events: none;
    }

    .now-line::before {
        content: '';
        position: absolute;
        left: -4px;
        top: -3px;
        width: 8px;
        height: 8px;
        border-radius: 50%;
        background: var(--danger);
    }

    .durations {
        display: flex;
        align-items: center;
        gap: 6px;
        flex-wrap: wrap;
        margin-bottom: 10px;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
    }

    .durations button {
        min-width: 44px;
        min-height: 40px;
        border: 1px solid var(--ctp-surface1);
        border-radius: 6px;
        background: color-mix(in srgb, var(--ctp-base) 60%, transparent);
        color: var(--ctp-text);
        font: inherit;
        cursor: pointer;
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .durations button.active {
        background: var(--ctp-mauve);
        border-color: var(--ctp-mauve);
        color: var(--accent-ink);
        font-weight: 600;
    }

    .form-warn {
        margin: 0 0 10px;
        font-size: 0.85rem;
        color: var(--warn);
    }

    .booking-block {
        position: absolute;
        left: 3px;
        right: 3px;
        box-sizing: border-box;
        box-shadow: var(--shadow-sm);
        animation: fade var(--normal) var(--ease);
        transition:
            transform var(--fast) var(--ease),
            filter var(--fast) var(--ease),
            box-shadow var(--fast) var(--ease);
        background: linear-gradient(135deg, color-mix(in srgb, var(--hue) 88%, var(--rp-text)), color-mix(in srgb, var(--hue) 68%, var(--rp-text)));
        color: var(--rp-base);
        border: none;
        border-radius: 6px;
        padding: 4px 6px;
        text-align: left;
        overflow: hidden;
        cursor: pointer;
        display: flex;
        flex-direction: column;
        gap: 1px;
        font: inherit;
        font-size: 0.7rem;
        line-height: 1.15;
    }

    .booking-block:hover {
        filter: brightness(1.08);
        transform: translateY(-1px);
        box-shadow: var(--shadow);
        z-index: 2;
    }

    /* Yours: a ring in the text colour */
    .booking-block.mine {
        box-shadow: inset 0 0 0 2px var(--ctp-text), var(--shadow-sm);
    }

    /* A request nobody has approved yet does not hold the slot */
    .booking-block.pending {
        background: color-mix(in srgb, var(--hue) 14%, color-mix(in srgb, var(--ctp-mantle) 60%, transparent));
        border: 1px dashed var(--hue);
        color: var(--ctp-text);
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .booking-block.noshow {
        border-left: 4px solid var(--danger);
    }

    .block-time {
        font-weight: 700;
    }

    .block-name {
        font-weight: 600;
    }

    .block-status {
        font-weight: 600;
    }

    .booking-block.pending .block-status {
        color: var(--warn);
    }

    .block-purpose {
        opacity: 0.85;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .loading-note {
        color: var(--ctp-overlay0);
        font-size: 0.85rem;
        text-align: center;
        margin-top: 12px;
    }

    .legend {
        display: flex;
        justify-content: center;
        flex-wrap: wrap;
        gap: 6px 18px;
        margin: 8px 0 0;
        font-size: 0.8rem;
        color: var(--ctp-subtext0);
    }

    .legend > span {
        display: inline-flex;
        align-items: center;
        gap: 6px;
    }

    .swatch {
        width: 14px;
        height: 10px;
        border-radius: 3px;
        background: linear-gradient(135deg, color-mix(in srgb, var(--hue) 88%, var(--rp-text)), color-mix(in srgb, var(--hue) 68%, var(--rp-text)));
    }

    .swatch.past {
        background: repeating-linear-gradient(135deg, transparent 0 3px, var(--line-strong) 3px 4px);
        border: 1px solid var(--line);
        box-sizing: border-box;
    }

    .swatch.pending {
        background: color-mix(in srgb, var(--hue) 14%, color-mix(in srgb, var(--ctp-mantle) 60%, transparent));
        border: 1px dashed var(--hue);
        box-sizing: border-box;
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    /* Rosé Pine Dawn: dark text reads better on the lighter hues */
    :global(:root[data-theme='light']) .booking-block:not(.pending) {
        color: var(--ctp-text);
        background: linear-gradient(135deg, color-mix(in srgb, var(--hue) 55%, color-mix(in srgb, var(--card) 60%, transparent)), color-mix(in srgb, var(--hue) 40%, color-mix(in srgb, var(--card) 60%, transparent)));
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    :global(:root[data-theme='light']) .swatch:not(.pending):not(.past) {
        background: color-mix(in srgb, var(--hue) 50%, color-mix(in srgb, var(--card) 60%, transparent));
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    :global(:root[data-theme='light']) .booking-block:not(.pending) .block-name,
    :global(:root[data-theme='light']) .booking-block:not(.pending) .block-time {
        color: var(--rp-text);
    }

    @media (max-width: 700px) {
        .calendar-head,
        .calendar-body {
            grid-template-columns: 40px repeat(7, minmax(70px, 1fr));
        }

        /* One horizontal scroller for the whole week; the page scrolls
           vertically, so the hour labels can stay pinned on the left. */
        .calendar-head,
        .calendar-body {
            min-width: 530px;
        }

        .calendar-body {
            max-height: none;
            overflow: visible;
        }

        .time-gutter {
            position: sticky;
            left: 0;
            z-index: 5;
            background: color-mix(in srgb, var(--ctp-mantle) 60%, transparent);
            -webkit-backdrop-filter: var(--glass-blur);
            backdrop-filter: var(--glass-blur);
        }

        .calendar {
            overflow-x: auto;
        }

        .block-purpose {
            display: none;
        }
    }
</style>
