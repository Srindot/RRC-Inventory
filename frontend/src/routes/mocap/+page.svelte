<script>
    import { onMount } from 'svelte';

    // Calendar shows this hour range, one row per hour
    const DAY_START = 8;
    const DAY_END = 22;
    const HOURS = Array.from({ length: DAY_END - DAY_START }, (_, i) => DAY_START + i);
    const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

    let bookings = [];
    let loading = false;
    let submitting = false;
    let message = '';
    let messageType = '';

    // Monday of the week currently on screen
    let weekStart = startOfWeek(new Date());
    let selectedBooking = null;
    let showForm = false;
    let cancelPhone = '';

    let form = {
        booked_by: '',
        phone: '',
        purpose: '',
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

    // Shared with the borrow form, so people fill this in once per device
    const CONTACT_KEY = 'rrc_contact';

    onMount(() => {
        try {
            const saved = JSON.parse(localStorage.getItem(CONTACT_KEY) || 'null');
            if (saved) {
                form = { ...form, booked_by: saved.name || '', phone: saved.phone || '' };
            }
        } catch (e) {
            // Ignore unreadable storage
        }
        loadBookings();
    });

    function saveContact() {
        try {
            localStorage.setItem(CONTACT_KEY, JSON.stringify({ name: form.booked_by, phone: form.phone }));
        } catch (e) {
            // Convenience only
        }
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

    // --- data ---
    async function loadBookings() {
        loading = true;
        try {
            const from = new Date(weekStart);
            const to = addDays(weekStart, 7);
            const response = await fetch(`/api/bookings?from=${from.toISOString()}&to=${to.toISOString()}`);
            if (response.ok) {
                bookings = await response.json();
            } else {
                showMessage('Failed to load bookings', 'error');
            }
        } catch (e) {
            showMessage('Failed to load bookings', 'error');
        } finally {
            loading = false;
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
        try {
            const response = await fetch('/api/bookings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    booked_by: form.booked_by,
                    phone: form.phone,
                    purpose: form.purpose,
                    start_time: start.toISOString(),
                    end_time: end.toISOString()
                })
            });

            const result = await response.json();
            if (response.ok) {
                saveContact();
                showMessage(result.message || 'Booked!', 'success');
                showForm = false;
                form = { ...form, purpose: '' };
                weekStart = startOfWeek(start);
                loadBookings();
            } else {
                showMessage(result.error || 'Failed to book the lab', 'error');
            }
        } catch (e) {
            showMessage('Network error. Please try again.', 'error');
        } finally {
            submitting = false;
        }
    }

    async function cancelBooking() {
        if (!cancelPhone.trim()) {
            showMessage('Enter the phone number used for the booking', 'error');
            return;
        }

        try {
            const response = await fetch(`/api/bookings/${selectedBooking.ID}/cancel`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ phone: cancelPhone.trim() })
            });

            const result = await response.json();
            if (response.ok) {
                showMessage('Booking cancelled', 'success');
                selectedBooking = null;
                cancelPhone = '';
                loadBookings();
            } else {
                showMessage(result.error || 'Failed to cancel booking', 'error');
            }
        } catch (e) {
            showMessage('Network error. Please try again.', 'error');
        }
    }

    // --- calendar layout ---
    // Bookings for one day, positioned by their start/end time
    function layOutDay(day, allBookings) {
        return allBookings
            .map((b) => ({ ...b, start: new Date(b.start_time), end: new Date(b.end_time) }))
            .filter((b) => sameDay(b.start, day))
            .map((b) => {
                const startHour = b.start.getHours() + b.start.getMinutes() / 60;
                const endHour = b.end.getHours() + b.end.getMinutes() / 60;
                const from = Math.max(startHour, DAY_START);
                const to = Math.min(endHour, DAY_END);
                return {
                    ...b,
                    top: ((from - DAY_START) / (DAY_END - DAY_START)) * 100,
                    height: (Math.max(to - from, 0.5) / (DAY_END - DAY_START)) * 100
                };
            });
    }

    function openFormAt(day, hour) {
        form = {
            ...form,
            date: toDateInput(day),
            start_hour: `${String(hour).padStart(2, '0')}:00`,
            end_hour: `${String(Math.min(hour + 2, DAY_END)).padStart(2, '0')}:00`
        };
        showForm = true;
        selectedBooking = null;
    }

    function goToWeek(offset) {
        weekStart = addDays(weekStart, offset * 7);
        loadBookings();
    }

    function goToday() {
        weekStart = startOfWeek(new Date());
        loadBookings();
    }

    function showMessage(text, type) {
        message = text;
        messageType = type;
        setTimeout(() => {
            message = '';
            messageType = '';
        }, 5000);
    }
</script>

<div class="container">
    <div class="header">
        <div class="logo-title">
            <a href="/"><img src="/rrc_logo.png" alt="RRC Logo" class="logo" /></a>
            <div>
                <h1>🎥 Motion Capture Lab</h1>
                <p class="subtitle">Book a slot - first come, first served</p>
            </div>
        </div>
        <a href="/" class="back-link">← Back to Home</a>
    </div>

    {#if message}
        <div class="message {messageType}">{message}</div>
    {/if}

    <div class="toolbar">
        <div class="week-nav">
            <button on:click={() => goToWeek(-1)} aria-label="Previous week">◀</button>
            <button class="today-btn" on:click={goToday}>Today</button>
            <button on:click={() => goToWeek(1)} aria-label="Next week">▶</button>
            <span class="week-label">{weekLabel}</span>
        </div>
        <button class="book-btn" on:click={() => { showForm = !showForm; selectedBooking = null; }}>
            {showForm ? '✕ Close' : '➕ Book a Slot'}
        </button>
    </div>

    {#if showForm}
        <form class="booking-form" on:submit|preventDefault={submitBooking}>
            <div class="form-row">
                <label>
                    Your name
                    <input type="text" bind:value={form.booked_by} required placeholder="Name" />
                </label>
                <label>
                    Phone
                    <input type="tel" bind:value={form.phone} required placeholder="Phone number" />
                </label>
            </div>
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
                <input type="text" bind:value={form.purpose} required placeholder="What are you using the lab for?" />
            </label>
            <p class="form-note">Your phone number is what you'll use to cancel the booking later.</p>
            <button type="submit" class="submit-btn" disabled={submitting}>
                {submitting ? 'Booking...' : 'Confirm Booking'}
            </button>
        </form>
    {/if}

    {#if selectedBooking}
        <div class="booking-details">
            <h3>{selectedBooking.purpose}</h3>
            <p><strong>Booked by:</strong> {selectedBooking.booked_by}</p>
            <p><strong>When:</strong> {new Date(selectedBooking.start_time).toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'short' })},
                {formatTime(selectedBooking.start_time)} - {formatTime(selectedBooking.end_time)}</p>
            <p><strong>Contact:</strong> {selectedBooking.phone}</p>
            <div class="details-actions">
                <input type="tel" bind:value={cancelPhone} placeholder="Your phone number to cancel" />
                {#if form.phone && cancelPhone !== form.phone}
                    <button class="close-btn" on:click={() => cancelPhone = form.phone}>Use my number</button>
                {/if}
                <button class="cancel-btn" on:click={cancelBooking}>Cancel Booking</button>
                <button class="close-btn" on:click={() => { selectedBooking = null; cancelPhone = ''; }}>Close</button>
            </div>
        </div>
    {/if}

    <div class="calendar">
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
                            on:click={() => openFormAt(column.day, hour)}
                            title="Book {String(hour).padStart(2, '0')}:00 on {column.day.toDateString()}"
                            aria-label="Book {String(hour).padStart(2, '0')}:00 on {column.day.toDateString()}"
                        ></button>
                    {/each}

                    {#each column.blocks as booking (booking.ID)}
                        <button
                            class="booking-block"
                            style="top: {booking.top}%; height: {booking.height}%;"
                            on:click={() => { selectedBooking = booking; showForm = false; }}
                        >
                            <span class="block-time">{formatTime(booking.start)} - {formatTime(booking.end)}</span>
                            <span class="block-name">{booking.booked_by}</span>
                            <span class="block-purpose">{booking.purpose}</span>
                        </button>
                    {/each}
                </div>
            {/each}
        </div>
    </div>

    {#if loading}
        <p class="loading-note">Loading bookings...</p>
    {:else if bookings.length === 0}
        <p class="loading-note">No bookings this week - the lab is all yours.</p>
    {/if}
</div>

<style>
    :global(body) {
        background: var(--ctp-base);
        color: var(--ctp-text);
        font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
        margin: 0;
        /* The home page locks the page height with overflow:hidden. That style
           stays loaded after client-side navigation, so undo it here or this
           page cannot be scrolled. */
        height: auto;
        min-height: 100vh;
        overflow-y: auto;
    }

    :global(html) {
        height: auto;
        overflow-y: auto;
    }

    .container {
        max-width: 1200px;
        margin: 0 auto;
        padding: 16px;
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

    .logo {
        height: 48px;
        width: auto;
    }

    h1 {
        margin: 0;
        font-size: clamp(1.2rem, 3vw, 1.6rem);
        color: var(--ctp-mauve);
    }

    .subtitle {
        margin: 2px 0 0;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
    }

    .back-link {
        color: var(--ctp-blue);
        text-decoration: none;
        font-size: 0.9rem;
    }

    .message {
        margin: 12px 0;
        padding: 10px 14px;
        border-radius: 8px;
        font-size: 0.9rem;
    }

    .message.success {
        background: #2a3b2a;
        color: var(--ctp-green);
        border: 1px solid var(--ctp-green);
    }

    .message.error {
        background: #3b2a2a;
        color: var(--ctp-red);
        border: 1px solid var(--ctp-red);
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
    }

    .week-nav button {
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        border: none;
        border-radius: 6px;
        padding: 8px 12px;
        cursor: pointer;
    }

    .week-nav button:hover {
        background: var(--ctp-surface1);
    }

    .week-label {
        color: var(--ctp-subtext0);
        font-size: 0.9rem;
        margin-left: 6px;
    }

    .book-btn {
        background: linear-gradient(135deg, var(--ctp-mauve), var(--ctp-lavender));
        color: var(--ctp-crust);
        border: none;
        border-radius: var(--radius-sm);
        padding: 10px 18px;
        font-weight: 650;
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

    .booking-form,
    .booking-details {
        background: var(--ctp-mantle);
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        padding: 16px;
        margin-bottom: 16px;
        box-shadow: var(--shadow-sm);
        animation: rise var(--normal) var(--ease) both;
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

    .booking-form input {
        background: var(--ctp-base);
        border: 1px solid var(--ctp-surface1);
        border-radius: 6px;
        padding: 9px;
        color: var(--ctp-text);
        font-size: 0.95rem;
    }

    .form-note {
        font-size: 0.8rem;
        color: var(--ctp-overlay0);
        margin: 0 0 10px;
    }

    .submit-btn {
        background: var(--ctp-green);
        color: var(--ctp-crust);
        border: none;
        border-radius: 8px;
        padding: 10px 18px;
        font-weight: 600;
        cursor: pointer;
    }

    .submit-btn:disabled {
        opacity: 0.6;
        cursor: not-allowed;
    }

    .booking-details h3 {
        margin: 0 0 8px;
        color: var(--ctp-mauve);
    }

    .booking-details p {
        margin: 4px 0;
        font-size: 0.9rem;
    }

    .details-actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
        margin-top: 12px;
    }

    .details-actions input {
        background: var(--ctp-base);
        border: 1px solid var(--ctp-surface1);
        border-radius: 6px;
        padding: 8px;
        color: var(--ctp-text);
        flex: 1;
        min-width: 160px;
    }

    .cancel-btn {
        background: var(--ctp-red);
        color: var(--ctp-crust);
        border: none;
        border-radius: 6px;
        padding: 8px 14px;
        cursor: pointer;
        font-weight: 600;
    }

    .close-btn {
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        border: none;
        border-radius: 6px;
        padding: 8px 14px;
        cursor: pointer;
    }

    /* --- calendar --- */
    .calendar {
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        overflow: hidden;
        background: var(--ctp-mantle);
        box-shadow: var(--shadow-sm);
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
        color: var(--ctp-crust);
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

    .day-column {
        position: relative;
        border-left: 1px solid var(--ctp-surface0);
    }

    .day-column.today {
        background: rgba(203, 166, 247, 0.06);
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
    }

    .hour-cell {
        transition: background-color var(--fast) var(--ease);
    }

    .hour-cell:hover {
        background: rgba(203, 166, 247, 0.16);
        box-shadow: inset 0 0 0 1px rgba(203, 166, 247, 0.35);
    }

    .booking-block {
        position: absolute;
        left: 3px;
        right: 3px;
        box-shadow: var(--shadow-sm);
        animation: fade var(--normal) var(--ease);
        transition:
            transform var(--fast) var(--ease),
            filter var(--fast) var(--ease),
            box-shadow var(--fast) var(--ease);
        background: linear-gradient(135deg, var(--ctp-mauve), var(--ctp-lavender));
        color: var(--ctp-crust);
        border: none;
        border-radius: 6px;
        padding: 4px 6px;
        text-align: left;
        overflow: hidden;
        cursor: pointer;
        display: flex;
        flex-direction: column;
        gap: 1px;
        font-size: 0.7rem;
        line-height: 1.15;
    }

    .booking-block:hover {
        filter: brightness(1.08);
        transform: translateY(-1px);
        box-shadow: var(--shadow);
        z-index: 2;
    }

    .block-time {
        font-weight: 700;
    }

    .block-name {
        font-weight: 600;
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
