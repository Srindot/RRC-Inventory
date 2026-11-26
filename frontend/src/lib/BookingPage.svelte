<script>
    // A week calendar for booking a lab or a robot, in the look of the original
    // Motion Capture Lab page. Used by /mocap (one resource) and /mechlab (one
    // tab per robot), and embedded in the admin page. Robots are booked for a
    // research thread (see /threads). Who may book, cancel or approve is
    // decided by the server; the page only hides buttons that would be refused.
    import { onMount, tick } from 'svelte';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import { AREA, hueFor } from '$lib/palette.js';
    import IconVideo from '@tabler/icons-svelte-runes/icons/video';
    import IconRobot from '@tabler/icons-svelte-runes/icons/robot';
    import IconChevronLeft from '@tabler/icons-svelte-runes/icons/chevron-left';
    import IconChevronRight from '@tabler/icons-svelte-runes/icons/chevron-right';
    import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
    import IconX from '@tabler/icons-svelte-runes/icons/x';
    import IconCheck from '@tabler/icons-svelte-runes/icons/check';
    import IconHourglass from '@tabler/icons-svelte-runes/icons/hourglass';
    import IconAlert from '@tabler/icons-svelte-runes/icons/alert-triangle';
    import IconRefresh from '@tabler/icons-svelte-runes/icons/refresh';
    import IconLock from '@tabler/icons-svelte-runes/icons/lock';
    import IconLogin from '@tabler/icons-svelte-runes/icons/login-2';
    import IconBan from '@tabler/icons-svelte-runes/icons/ban';
    import IconFlask from '@tabler/icons-svelte-runes/icons/flask';
    import IconCircleCheck from '@tabler/icons-svelte-runes/icons/circle-check';
    import IconClock from '@tabler/icons-svelte-runes/icons/clock';
    import IconCalendar from '@tabler/icons-svelte-runes/icons/calendar-event';
    import IconCalendarOff from '@tabler/icons-svelte-runes/icons/calendar-off';
    import IconStar from '@tabler/icons-svelte-runes/icons/star-filled';
    import IconList from '@tabler/icons-svelte-runes/icons/list-details';
    import IconInfo from '@tabler/icons-svelte-runes/icons/info-circle';
    import IconUserX from '@tabler/icons-svelte-runes/icons/user-x';

    export let lab; // 'mocap' | 'mech'
    export let title;
    export let subtitle = '';
    export let embedded = false; // inside the admin page: no page header

    const LAB_ICON = { mocap: IconVideo, mech: IconRobot };

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
    // On a phone the calendar shows one day at a time: this one (0 = Monday)
    let dayIdx = weekdayIndex(new Date());
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

    // Monday = 0, matching the calendar's columns
    function weekdayIndex(date) {
        return (new Date(date).getDay() + 6) % 7;
    }

    // On a phone (one day on screen) start at today when it is in this week
    function scrollToToday() {
        if (weekDays.some(isToday)) dayIdx = weekdayIndex(new Date());
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
            dayIdx = weekdayIndex(start);
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
        dayIdx = weekdayIndex(b.start_time);
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

<div class="booking" style="--hue: {labHue}">
    {#if !embedded}
        <div class="page-header d-print-none mb-3">
            <div class="row g-3 align-items-center">
                <div class="col-12 col-md">
                    <div class="page-pretitle">Lab booking</div>
                    <h2 class="page-title">
                        <span class="lab-icon me-2"><svelte:component this={LAB_ICON[lab]} size={24} class="icon" /></span>
                        {title}
                    </h2>
                    {#if subtitle}<div class="text-secondary mt-1">{subtitle}</div>{/if}
                </div>
                <div class="col-12 col-md-auto">
                    <nav class="nav nav-segmented w-100" aria-label="Booking pages">
                        <a class="nav-link" href="/mechlab" class:active={lab === 'mech'} aria-current={lab === 'mech' ? 'page' : undefined}>
                            <IconRobot size={18} class="icon nav-link-icon" />Mech Lab
                        </a>
                        <a class="nav-link" href="/mocap" class:active={lab === 'mocap'} aria-current={lab === 'mocap' ? 'page' : undefined}>
                            <IconVideo size={18} class="icon nav-link-icon" />MoCap Lab
                        </a>
                    </nav>
                </div>
            </div>
        </div>
    {:else}
        <h3 class="d-flex align-items-center gap-2 mb-3">
            <span class="lab-icon"><svelte:component this={LAB_ICON[lab]} size={20} class="icon" /></span>
            {title}
        </h3>
    {/if}

    {#if message}
        <div class="toast-msg {messageType}" role="status">{message}</div>
    {/if}

    {#if approvable.length > 0}
        <div class="card mb-3">
            <div class="card-header">
                <h3 class="card-title d-flex align-items-center gap-2">
                    <IconHourglass size={18} class="icon text-warning" />
                    Waiting for your approval
                    <span class="badge bg-warning-lt">{approvable.length}</span>
                </h3>
            </div>
            <div class="table-responsive">
                <table class="table table-vcenter card-table">
                    <thead>
                        <tr>
                            <th>Who</th>
                            <th>{lab === 'mech' ? 'Robot' : 'Lab'}</th>
                            <th>When</th>
                            <th>Purpose</th>
                            <th class="w-1"><span class="visually-hidden">Actions</span></th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each approvable as b (b.ID)}
                            <tr>
                                <td>
                                    <div class="d-flex align-items-center">
                                        <span class="dot" style="--hue: {blockHue(b)}"></span>
                                        <div>
                                            <div class="fw-medium">{b.booked_by}</div>
                                            {#if b.thread_name}<div class="small text-secondary">{b.thread_name}</div>{/if}
                                        </div>
                                    </div>
                                </td>
                                <td>{resourceName(b.resource_id)}</td>
                                <td class="text-secondary text-nowrap">{formatWhen(b)}</td>
                                <td class="text-secondary">{b.purpose || ''}</td>
                                <td>
                                    <div class="btn-list flex-nowrap">
                                        <button class="btn btn-success" type="button" on:click={() => act(`/api/bookings/${b.ID}/approve`, 'Approved')}>
                                            <IconCheck size={18} class="icon" />Approve
                                        </button>
                                        <button class="btn btn-outline-danger" type="button" on:click={() => act(`/api/bookings/${b.ID}/reject`, 'Declined')}>
                                            <IconX size={18} class="icon" />Decline
                                        </button>
                                    </div>
                                </td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            </div>
        </div>
    {/if}

    {#if resourcesState === 'error'}
        <div class="card">
            <div class="card-body">
                <div class="empty">
                    <div class="empty-icon text-danger"><IconAlert size={48} class="icon" /></div>
                    <p class="empty-title">Couldn't load the booking calendar</p>
                    <p class="empty-subtitle text-secondary">The server didn't answer. Check your connection and try again.</p>
                    <div class="empty-action">
                        <button class="btn btn-primary" type="button" on:click={async () => { await loadResources(); loadSidePanels(); }}>
                            <IconRefresh size={18} class="icon" />Try again
                        </button>
                    </div>
                </div>
            </div>
        </div>
    {:else if resourcesState === 'loading'}
        <div class="card">
            <div class="card-body d-flex align-items-center justify-content-center gap-2 text-secondary py-5">
                <span class="spinner-border spinner-border-sm" aria-hidden="true"></span>Loading...
            </div>
        </div>
    {:else if !resource}
        <div class="card">
            <div class="card-body">
                <div class="empty">
                    <div class="empty-icon"><svelte:component this={LAB_ICON[lab]} size={48} class="icon" /></div>
                    <p class="empty-title">{lab === 'mech' ? 'No robots yet' : 'Not open for booking yet'}</p>
                    <p class="empty-subtitle text-secondary">
                        {lab === 'mech'
                            ? 'No robots have been added yet. A lab admin adds them from the admin page.'
                            : "The Motion Capture Lab hasn't been set up for booking yet. A lab admin adds it from the admin page."}
                    </p>
                </div>
            </div>
        </div>
    {:else}
        {#if resources.length > 1}
            <div class="mb-3 robot-tabs">
                <div class="nav nav-segmented" role="tablist" aria-label="Robots">
                    {#each resources as r (r.id)}
                        <button
                            type="button"
                            role="tab"
                            class="nav-link"
                            aria-selected={resource?.id === r.id}
                            class:active={resource?.id === r.id}
                            on:click={() => selectResource(r)}
                        >
                            <span class="dot me-0" style="--hue: {hueFor(r.id)}"></span>{r.name}
                        </button>
                    {/each}
                </div>
            </div>
        {/if}

        {#if !user}
            <div class="alert alert-info mb-3" role="alert">
                <div class="d-flex flex-wrap align-items-center gap-3">
                    <div class="alert-icon"><IconLock size={24} class="icon" /></div>
                    <div class="flex-fill">
                        Anyone can see the calendar. <strong>To book a slot, log in with your lab account</strong> - new here? <a href="/register">Create an account</a>.
                    </div>
                    <a class="btn btn-primary" href={loginUrl()}><IconLogin size={18} class="icon" />Log in to book</a>
                </div>
            </div>
        {:else if noPermission}
            <div class="alert alert-warning mb-3" role="alert">
                <div class="d-flex">
                    <div class="alert-icon"><IconBan size={24} class="icon" /></div>
                    <div>Your account can't book {lab === 'mech' ? 'robots' : 'this lab'} yet - ask a lab admin to give your group access.</div>
                </div>
            </div>
        {:else if noThread}
            <div class="alert alert-warning mb-3" role="alert">
                <div class="d-flex">
                    <div class="alert-icon"><IconFlask size={24} class="icon" /></div>
                    <div>
                        None of your research threads can book <strong>{resource.name}</strong> yet.
                        {#if can(user, 'create_threads')}
                            Request access for your thread on the <a href="/threads">Threads page</a>.
                        {:else}
                            Robots are booked through research threads (a prof's project). Ask your prof to add you to theirs - see who leads what on the <a href="/threads">Threads page</a>.
                        {/if}
                    </div>
                </div>
            </div>
        {/if}

        <div class="row g-3">
            <div class="col-12 col-xl-8">
                <div class="card">
                    <div class="card-header flex-wrap gap-2">
                        <div class="d-flex align-items-center gap-2 flex-wrap me-auto">
                            <div class="btn-group" role="group" aria-label="Week">
                                <button class="btn btn-icon" type="button" on:click={() => goToWeek(-1)} aria-label="Previous week">
                                    <IconChevronLeft size={18} class="icon" />
                                </button>
                                <button class="btn" type="button" on:click={goToday}>Today</button>
                                <button class="btn btn-icon" type="button" on:click={() => goToWeek(1)} aria-label="Next week">
                                    <IconChevronRight size={18} class="icon" />
                                </button>
                            </div>
                            <span class="text-secondary week-label">{weekLabel}</span>
                        </div>
                        {#if mayBook}
                            <button class="btn {showForm ? 'btn-outline-secondary' : 'btn-primary'}" type="button" on:click={() => { showForm = !showForm; selectedBooking = null; }}>
                                {#if showForm}<IconX size={18} class="icon" />Close{:else}<IconPlus size={18} class="icon" />Book a slot{/if}
                            </button>
                        {/if}
                    </div>

                    <!-- Phones: one day at a time, picked here -->
                    <div class="d-md-none border-bottom p-2">
                        <div class="nav nav-segmented w-100 day-strip" role="tablist" aria-label="Day">
                            {#each columns as column, i}
                                <button
                                    type="button"
                                    role="tab"
                                    class="nav-link"
                                    class:active={dayIdx === i}
                                    class:is-today={isToday(column.day)}
                                    aria-selected={dayIdx === i}
                                    aria-label="{column.day.toDateString()}{column.blocks.length ? `, ${column.blocks.length} booked` : ''}"
                                    on:click={() => (dayIdx = i)}
                                >
                                    <span class="strip-name">{DAY_NAMES[column.day.getDay()]}</span>
                                    <span class="strip-num">{column.day.getDate()}</span>
                                    <span class="strip-mark" class:has={column.blocks.length > 0}></span>
                                </button>
                            {/each}
                        </div>
                    </div>

                    <div class="calendar">
                        <div class="cal-grid cal-head">
                            <div class="gutter"></div>
                            {#each weekDays as day, i}
                                <div class="day-head" class:today={isToday(day)} class:off={dayIdx !== i}>
                                    <span class="day-name">{DAY_NAMES[day.getDay()]}</span>
                                    <span class="day-number">{day.getDate()}</span>
                                </div>
                            {/each}
                        </div>

                        <div class="cal-grid cal-body">
                            <div class="gutter">
                                {#each HOURS as hour}
                                    <div class="time-label">{String(hour).padStart(2, '0')}:00</div>
                                {/each}
                            </div>

                            {#each columns as column, i}
                                <div class="day-column" class:today={isToday(column.day)} class:off={dayIdx !== i}>
                                    {#each HOURS as hour}
                                        <button
                                            type="button"
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
                                            type="button"
                                            class="booking-block"
                                            class:pending={booking.status === 'pending'}
                                            class:mine={isMine(booking)}
                                            class:noshow={booking.no_show}
                                            style="top: {booking.top}%; height: {booking.height}%; --hue: {blockHue(booking)};"
                                            on:click={() => showBooking(booking)}
                                        >
                                            <span class="block-time">{formatTime(booking.start)} - {formatTime(booking.end)}{booking.outside ? ` (${booking.outside})` : ''}</span>
                                            <span class="block-name">{#if isMine(booking)}<span class="visually-hidden">Yours: </span><IconStar size={11} class="me-1" />{/if}{booking.booked_by}{booking.thread_name ? ` · ${booking.thread_name}` : ''}</span>
                                            {#if booking.status === 'pending'}<span class="block-status">Pending</span>{/if}
                                            {#if booking.no_show}<span class="block-status">No-show</span>{/if}
                                            <span class="block-purpose">{booking.purpose}</span>
                                        </button>
                                    {/each}
                                </div>
                            {/each}
                        </div>
                    </div>

                    <div class="card-footer">
                        {#if loading}
                            <div class="text-secondary small mb-2 d-flex align-items-center gap-2">
                                <span class="spinner-border spinner-border-sm" aria-hidden="true"></span>Loading bookings...
                            </div>
                        {:else if bookingsError}
                            <div class="text-danger small mb-2">
                                Couldn't load this week's bookings.
                                <button class="btn btn-link btn-sm p-0 align-baseline" type="button" on:click={loadBookings}>Try again</button>
                            </div>
                        {:else if bookings.length === 0}
                            <div class="text-secondary small mb-2">No bookings this week - the {lab === 'mech' ? 'robot' : 'lab'} is all yours.</div>
                        {/if}
                        <div class="legend small text-secondary">
                            <span><span class="swatch"></span>Booked</span>
                            <span><span class="swatch pending"></span>Pending approval</span>
                            {#if user}<span><IconStar size={12} class="icon" />Yours</span>{/if}
                            <span><span class="swatch past"></span>Past</span>
                            {#if mayBook || !user}<span>Tap a free hour to {user ? 'book it' : 'log in and book'}</span>{/if}
                        </div>
                    </div>
                </div>
            </div>

            <div class="col-12 col-xl-4">
                <div class="d-flex flex-column gap-3">
                    {#if showForm}
                        <form class="card" bind:this={formEl} on:submit|preventDefault={submitBooking}>
                            <div class="card-header">
                                <h3 class="card-title d-flex align-items-center gap-2">
                                    <IconCalendar size={18} class="icon" />New booking
                                    <span class="text-secondary fw-normal">· {resource.name}</span>
                                </h3>
                            </div>
                            <div class="card-body">
                                <p class="text-secondary small mb-3">
                                    Booking as <strong class="text-reset">{user?.name || user?.email}</strong>{user?.phone ? ` (${user.phone})` : ''}.
                                </p>
                                <div class="row g-2 mb-3">
                                    <div class="col-12 col-sm-4 col-xl-12">
                                        <label class="form-label" for="bk-date-{lab}">Date</label>
                                        <input id="bk-date-{lab}" class="form-control" type="date" bind:value={form.date} required />
                                    </div>
                                    <div class="col-6 col-sm-4 col-xl-6">
                                        <label class="form-label" for="bk-from-{lab}">From</label>
                                        <input id="bk-from-{lab}" class="form-control" type="time" bind:value={form.start_hour} required step="900" />
                                    </div>
                                    <div class="col-6 col-sm-4 col-xl-6">
                                        <label class="form-label" for="bk-to-{lab}">To</label>
                                        <input id="bk-to-{lab}" class="form-control" type="time" bind:value={form.end_hour} required step="900" />
                                    </div>
                                </div>
                                <div class="mb-3">
                                    <div class="form-label">Length</div>
                                    <div class="btn-list" role="group" aria-label="Length">
                                        {#each durations as h}
                                            <button
                                                type="button"
                                                class="btn {formHours === h ? 'btn-primary' : 'btn-outline-secondary'} length-btn"
                                                aria-pressed={formHours === h}
                                                on:click={() => setDuration(h)}
                                            >{h} h</button>
                                        {/each}
                                    </div>
                                </div>
                                {#if tooLong}
                                    <div class="alert alert-warning py-2 mb-3" role="alert">
                                        <div class="d-flex">
                                            <div class="alert-icon"><IconAlert size={20} class="icon" /></div>
                                            <div>{resource.name} can be booked for at most {resource.max_hours} h at a time.</div>
                                        </div>
                                    </div>
                                {/if}
                                {#if clashes.length}
                                    <div class="alert alert-warning py-2 mb-3" role="alert">
                                        <div class="d-flex">
                                            <div class="alert-icon"><IconAlert size={20} class="icon" /></div>
                                            <div>Overlaps {clashes.map((b) => `${b.booked_by} (${formatTime(b.start_time)} - ${formatTime(b.end_time)}${b.status === 'pending' ? ', pending' : ''})`).join(', ')}.</div>
                                        </div>
                                    </div>
                                {/if}
                                <div class="mb-3">
                                    <label class="form-label" for="bk-purpose-{lab}">Purpose</label>
                                    <input id="bk-purpose-{lab}" class="form-control" type="text" bind:value={form.purpose} placeholder="What are you using the {lab === 'mech' ? 'robot' : 'lab'} for? (optional)" />
                                </div>
                                {#if can(user, 'manage_bookings')}
                                    <div class="row g-2 mb-3">
                                        <div class="col-12 col-sm-7 col-xl-12">
                                            <label class="form-label" for="bk-behalf-{lab}">Booking for someone else? Their name</label>
                                            <input id="bk-behalf-{lab}" class="form-control" type="text" bind:value={form.on_behalf_name} placeholder="Leave empty to book for yourself" />
                                        </div>
                                        <div class="col-12 col-sm-5 col-xl-12">
                                            <label class="form-label" for="bk-phone-{lab}">Their phone</label>
                                            <input id="bk-phone-{lab}" class="form-control" type="tel" bind:value={form.on_behalf_phone} placeholder="Optional" />
                                        </div>
                                    </div>
                                {/if}
                                {#if resource.requires_thread || threadChoices.length}
                                    <div class="mb-3">
                                        <label class="form-label" for="bk-thread-{lab}">Research thread</label>
                                        <select id="bk-thread-{lab}" class="form-select" bind:value={form.thread_id} required={resource.requires_thread}>
                                            <option value="">{resource.requires_thread ? 'Pick a thread' : 'None'}</option>
                                            {#each threadChoices as t (t.ID)}
                                                <option value={t.ID}>{t.name} - {allowance(t)}</option>
                                            {/each}
                                        </select>
                                        {#if pickedThread}
                                            <div class="form-hint d-flex align-items-center">
                                                <span class="dot" style="--hue: {hueFor(pickedThread.ID)}"></span>{pickedThread.name}: {allowance(pickedThread)}
                                            </div>
                                        {/if}
                                    </div>
                                {/if}
                                {#if needsApproval}
                                    <div class="form-hint d-flex align-items-center gap-1">
                                        <IconHourglass size={16} class="icon" />A lab admin approves this request. You'll get a notification.
                                    </div>
                                {/if}
                            </div>
                            <div class="card-footer">
                                <button type="submit" class="btn btn-primary w-100" disabled={submitting}>
                                    {submitting ? 'Booking...' : needsApproval ? 'Request booking' : 'Confirm booking'}
                                </button>
                            </div>
                        </form>
                    {/if}

                    {#if selectedBooking}
                        <div class="card hue-edge" bind:this={detailsEl} style="--hue: {blockHue(selectedBooking)}">
                            <div class="card-header">
                                <h3 class="card-title">{selectedBooking.purpose || 'Booking'}</h3>
                                <div class="card-actions">
                                    <button class="btn btn-ghost-secondary btn-icon" type="button" aria-label="Close" on:click={() => (selectedBooking = null)}>
                                        <IconX size={18} class="icon" />
                                    </button>
                                </div>
                            </div>
                            <div class="card-body">
                                <div class="datagrid">
                                    <div class="datagrid-item">
                                        <div class="datagrid-title">Booked by</div>
                                        <div class="datagrid-content">{selectedBooking.booked_by}{isMine(selectedBooking) ? ' (you)' : ''}</div>
                                    </div>
                                    {#if resources.length > 1}
                                        <div class="datagrid-item">
                                            <div class="datagrid-title">Robot</div>
                                            <div class="datagrid-content">{resourceName(selectedBooking.resource_id)}</div>
                                        </div>
                                    {/if}
                                    {#if selectedBooking.thread_name}
                                        <div class="datagrid-item">
                                            <div class="datagrid-title">Thread</div>
                                            <div class="datagrid-content d-flex align-items-center">
                                                <span class="dot" style="--hue: {blockHue(selectedBooking)}"></span>{selectedBooking.thread_name}
                                            </div>
                                        </div>
                                    {/if}
                                    <div class="datagrid-item">
                                        <div class="datagrid-title">When</div>
                                        <div class="datagrid-content">{formatWhen(selectedBooking)}</div>
                                    </div>
                                </div>
                                {#if selectedBooking.status === 'pending' || selectedBooking.no_show}
                                    <div class="d-flex flex-wrap gap-2 mt-3">
                                        {#if selectedBooking.status === 'pending'}<span class="badge bg-warning-lt"><IconHourglass size={14} class="icon me-1" />Waiting for approval</span>{/if}
                                        {#if selectedBooking.no_show}<span class="badge bg-danger-lt"><IconUserX size={14} class="icon me-1" />Reported as a no-show</span>{/if}
                                    </div>
                                {/if}
                            </div>
                            <div class="card-footer">
                                <div class="btn-list">
                                    {#if mayCancel(selectedBooking)}
                                        <button class="btn btn-danger" type="button" on:click={() => cancelBooking(selectedBooking)}>Cancel booking</button>
                                    {/if}
                                    {#if selectedBooking.status === 'pending' && can(user, 'approve_mech_bookings') && !isMine(selectedBooking)}
                                        <button class="btn btn-success" type="button" on:click={() => act(`/api/bookings/${selectedBooking.ID}/approve`, 'Approved')}>
                                            <IconCheck size={18} class="icon" />Approve
                                        </button>
                                        <button class="btn btn-outline-danger" type="button" on:click={() => act(`/api/bookings/${selectedBooking.ID}/reject`, 'Declined')}>
                                            <IconX size={18} class="icon" />Decline
                                        </button>
                                    {/if}
                                    {#if mayReportNoShow(selectedBooking)}
                                        <button class="btn btn-outline-secondary" type="button" on:click={() => confirm(`Report that ${selectedBooking.booked_by} didn't turn up? Your name is recorded with the report.`) && act(`/api/bookings/${selectedBooking.ID}/no-show`, 'No-show recorded')}>They didn't turn up</button>
                                    {/if}
                                    {#if selectedBooking.no_show && can(user, 'manage_bookings')}
                                        <button class="btn btn-outline-secondary" type="button" on:click={() => act(`/api/admin/bookings/${selectedBooking.ID}/clear-no-show`, 'No-show cleared')}>Clear no-show</button>
                                    {/if}
                                    {#if !user}
                                        <a class="btn btn-outline-primary" href={loginUrl()}>Log in to cancel your booking</a>
                                    {/if}
                                    <button class="btn btn-ghost-secondary" type="button" on:click={() => (selectedBooking = null)}>Close</button>
                                </div>
                            </div>
                        </div>
                    {/if}

                    {#if myUpcoming.length > 0}
                        <div class="card">
                            <div class="card-header">
                                <h3 class="card-title d-flex align-items-center gap-2">
                                    <IconList size={18} class="icon" />Your upcoming bookings
                                    <span class="badge bg-primary-lt">{myUpcoming.length}</span>
                                </h3>
                            </div>
                            <div class="list-group list-group-flush">
                                {#each myUpcoming as b (b.ID)}
                                    <div class="list-group-item">
                                        <div class="d-flex align-items-center gap-2">
                                            <button class="row-link flex-fill" type="button" on:click={() => jumpTo(b)} title="Show on the calendar">
                                                <span class="d-flex align-items-center fw-medium">
                                                    <span class="dot" style="--hue: {blockHue(b)}"></span>
                                                    {#if resources.length > 1}{resourceName(b.resource_id)}{:else}{formatWhen(b)}{/if}
                                                </span>
                                                {#if resources.length > 1 || b.thread_name || b.purpose}
                                                    <span class="d-block small text-secondary">
                                                        {[resources.length > 1 ? formatWhen(b) : '', b.thread_name, b.purpose].filter(Boolean).join(' · ')}
                                                    </span>
                                                {/if}
                                                {#if b.status === 'pending' || b.no_show}
                                                    <span class="d-flex flex-wrap gap-1 mt-1">
                                                        {#if b.status === 'pending'}<span class="badge bg-warning-lt">Pending</span>{/if}
                                                        {#if b.no_show}<span class="badge bg-danger-lt">No-show</span>{/if}
                                                    </span>
                                                {/if}
                                            </button>
                                            {#if mayCancel(b)}
                                                <button class="btn btn-outline-danger" type="button" on:click={() => cancelBooking(b)}>Cancel</button>
                                            {/if}
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        </div>
                    {/if}

                    {#if resource.requires_approval || resource.requires_thread || resource.max_hours || resource.max_future_bookings}
                        <div class="card">
                            <div class="card-header">
                                <h3 class="card-title d-flex align-items-center gap-2"><IconInfo size={18} class="icon" />How booking works here</h3>
                            </div>
                            <div class="list-group list-group-flush">
                                {#if resource.requires_thread}
                                    <div class="list-group-item d-flex align-items-center gap-2"><IconFlask size={18} class="icon text-secondary" />Booked by research threads with access</div>
                                {/if}
                                {#if resource.requires_approval}
                                    <div class="list-group-item d-flex align-items-center gap-2"><IconCircleCheck size={18} class="icon text-secondary" />Each booking is approved by a lab admin</div>
                                {/if}
                                {#if resource.max_hours}
                                    <div class="list-group-item d-flex align-items-center gap-2"><IconClock size={18} class="icon text-secondary" />Up to {resource.max_hours} h per booking</div>
                                {/if}
                                {#if resource.max_future_bookings}
                                    <div class="list-group-item d-flex align-items-center gap-2"><IconCalendarOff size={18} class="icon text-secondary" />At most {resource.max_future_bookings} upcoming per person</div>
                                {/if}
                            </div>
                        </div>
                    {/if}
                </div>
            </div>
        </div>
    {/if}
</div>

<style>
    .lab-icon {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        width: 2.25rem;
        height: 2.25rem;
        border-radius: var(--tblr-border-radius);
        background: color-mix(in srgb, var(--hue) 18%, transparent);
        color: var(--tblr-body-color);
        vertical-align: middle;
    }

    /* A thread's or robot's colour, as on the Threads page */
    .dot {
        display: inline-block;
        flex: none;
        width: 0.6rem;
        height: 0.6rem;
        margin-right: 0.5rem;
        border-radius: 50%;
        background: var(--hue);
    }

    .robot-tabs .nav-segmented {
        max-width: 100%;
    }

    .week-label {
        font-variant-numeric: tabular-nums;
    }

    .length-btn {
        min-width: 3.25rem;
    }

    .hue-edge {
        border-left: 4px solid var(--hue);
    }

    .row-link {
        display: block;
        min-width: 0;
        min-height: 2.5rem;
        padding: 0;
        border: 0;
        background: none;
        color: inherit;
        font: inherit;
        text-align: left;
        cursor: pointer;
    }

    .row-link:hover .fw-medium {
        color: var(--tblr-primary);
    }

    /* --- phone day strip --- */
    .day-strip {
        flex-wrap: nowrap;
        --tblr-nav-height: 3.5rem;
        --tblr-nav-link-padding-x: 0.25rem;
    }

    .day-strip .nav-link {
        flex: 1 1 0;
        min-width: 0;
        flex-direction: column;
        gap: 0;
        line-height: 1.1;
    }

    .strip-name {
        font-size: 0.7rem;
        text-transform: uppercase;
    }

    .strip-num {
        font-weight: 600;
    }

    .day-strip .is-today .strip-num {
        color: var(--tblr-primary);
    }

    .strip-mark {
        width: 4px;
        height: 4px;
        margin-top: 2px;
        border-radius: 50%;
    }

    .strip-mark.has {
        background: var(--tblr-primary);
    }

    /* --- calendar --- */
    .cal-grid {
        display: grid;
        grid-template-columns: 3.5rem repeat(7, minmax(0, 1fr));
    }

    .cal-head {
        border-bottom: 1px solid var(--tblr-border-color);
    }

    .day-head {
        display: flex;
        flex-direction: column;
        align-items: center;
        padding: 0.5rem 0.125rem;
        border-left: 1px solid var(--tblr-border-color);
    }

    .day-name {
        font-size: 0.7rem;
        color: var(--tblr-secondary-color);
        text-transform: uppercase;
    }

    .day-number {
        width: 1.75rem;
        height: 1.75rem;
        line-height: 1.75rem;
        text-align: center;
        font-size: 1rem;
        font-weight: 600;
    }

    .day-head.today .day-number {
        border-radius: 50%;
        background: var(--tblr-primary);
        color: var(--tblr-primary-fg);
    }

    .cal-body {
        position: relative;
        max-height: 60vh;
        overflow-y: auto;
    }

    .gutter {
        border-right: 1px solid var(--tblr-border-color);
    }

    .cal-head .gutter {
        border-right: 0;
    }

    .time-label {
        height: 3rem;
        padding-right: 0.375rem;
        font-size: 0.7rem;
        color: var(--tblr-secondary-color);
        text-align: right;
        font-variant-numeric: tabular-nums;
        transform: translateY(-0.4rem);
    }

    /* The first label sits on the top edge; shifting it up would clip it */
    .time-label:first-child {
        transform: none;
    }

    .day-column {
        position: relative;
        border-left: 1px solid var(--tblr-border-color);
    }

    .cal-body .gutter + .day-column {
        border-left: 0;
    }

    .day-column.today {
        background: color-mix(in srgb, var(--tblr-primary) 6%, transparent);
    }

    .hour-cell {
        display: block;
        width: 100%;
        height: 3rem;
        padding: 0;
        border: 0;
        border-bottom: 1px solid var(--tblr-border-color);
        background: transparent;
        cursor: pointer;
        transition: background-color 0.15s;
    }

    .hour-cell:hover,
    .hour-cell:focus-visible {
        background: color-mix(in srgb, var(--tblr-primary) 16%, transparent);
        outline: none;
        box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--tblr-primary) 40%, transparent);
    }

    .hour-cell.past {
        cursor: default;
        background: repeating-linear-gradient(135deg, transparent 0 6px, var(--tblr-border-color) 6px 7px);
        box-shadow: none;
    }

    .now-line {
        position: absolute;
        left: 0;
        right: 0;
        z-index: 3;
        height: 2px;
        background: var(--tblr-danger);
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
        background: var(--tblr-danger);
    }

    /* A booking in its thread's colour: dark ink on the pale Rosé Pine hues */
    .booking-block {
        position: absolute;
        left: 3px;
        right: 3px;
        display: flex;
        flex-direction: column;
        gap: 1px;
        padding: 0.25rem 0.375rem;
        overflow: hidden;
        border: 0;
        border-radius: var(--tblr-border-radius);
        background: color-mix(in srgb, var(--hue) 82%, var(--tblr-body-color));
        color: var(--rp-base);
        font: inherit;
        font-size: 0.7rem;
        line-height: 1.15;
        text-align: left;
        cursor: pointer;
        box-shadow: var(--tblr-box-shadow-sm, 0 1px 2px rgba(0, 0, 0, 0.1));
        transition: filter 0.15s, transform 0.15s;
    }

    .booking-block:hover,
    .booking-block:focus-visible {
        z-index: 2;
        filter: brightness(1.06);
        transform: translateY(-1px);
    }

    /* Yours: a ring in the text colour */
    .booking-block.mine {
        box-shadow: inset 0 0 0 2px var(--tblr-body-color);
    }

    /* A request nobody has approved yet does not hold the slot */
    .booking-block.pending {
        border: 1px dashed var(--hue);
        background: color-mix(in srgb, var(--hue) 14%, var(--tblr-bg-surface));
        color: var(--tblr-body-color);
    }

    .booking-block.noshow {
        border-left: 4px solid var(--tblr-danger);
    }

    .block-time {
        font-weight: 700;
    }

    .block-name,
    .block-status {
        font-weight: 600;
    }

    .booking-block.pending .block-status {
        color: var(--tblr-warning);
    }

    .block-purpose {
        overflow: hidden;
        white-space: nowrap;
        text-overflow: ellipsis;
        opacity: 0.85;
    }

    /* Rosé Pine Dawn: the hues are darker, so a lighter fill with body ink */
    :global(:root[data-theme='light']) .booking-block:not(.pending) {
        background: color-mix(in srgb, var(--hue) 45%, var(--tblr-bg-surface));
        color: var(--tblr-body-color);
    }

    .legend {
        display: flex;
        flex-wrap: wrap;
        gap: 0.375rem 1.25rem;
    }

    .legend > span {
        display: inline-flex;
        align-items: center;
        gap: 0.375rem;
    }

    .swatch {
        width: 14px;
        height: 10px;
        border-radius: 3px;
        background: color-mix(in srgb, var(--hue) 82%, var(--tblr-body-color));
    }

    :global(:root[data-theme='light']) .swatch:not(.pending):not(.past) {
        background: color-mix(in srgb, var(--hue) 45%, var(--tblr-bg-surface));
    }

    .swatch.pending {
        border: 1px dashed var(--hue);
        background: color-mix(in srgb, var(--hue) 14%, var(--tblr-bg-surface));
    }

    .swatch.past {
        border: 1px solid var(--tblr-border-color);
        background: repeating-linear-gradient(135deg, transparent 0 3px, var(--tblr-border-color) 3px 4px);
    }

    /* Phones: one day column, picked from the strip above */
    @media (max-width: 767.98px) {
        .cal-grid {
            grid-template-columns: 3rem minmax(0, 1fr);
        }

        .day-head.off,
        .day-column.off {
            display: none;
        }

        .cal-body {
            max-height: none;
            overflow: visible;
        }

        .day-column {
            border-left: 0;
        }

        .booking-block {
            font-size: 0.8rem;
            padding: 0.375rem 0.5rem;
        }
    }
</style>
