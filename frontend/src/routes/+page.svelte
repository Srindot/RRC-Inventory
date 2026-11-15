<script>
    import { onMount } from 'svelte';
    import { browser } from '$app/environment';
    import { pushState, replaceState } from '$app/navigation';
    import { page } from '$app/stores';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import Icon from '$lib/Icon.svelte';
    import { AREA } from '$lib/palette.js';

    let user = null;
    // True once we know whether the session is still alive
    let ready = false;

    const VIEWS = ['home', 'borrow', 'return', 'printer-guide'];

    // The view lives in the URL (?view=) and in shallow-routing state, so the
    // phone's back button returns home instead of leaving the site.
    // /?borrow=<item> comes from the catalogue page's Borrow button.
    // (url.search can't be read while prerendering, hence the browser check.)
    $: params = browser ? $page.url.searchParams : new URLSearchParams();
    $: urlView = VIEWS.includes(params.get('view')) ? params.get('view') : params.has('borrow') ? 'borrow' : 'home';
    $: currentView = $page.state.view || urlView;

    onMount(async () => {
        user = getUser();
        user = await refreshUser();
        ready = true;
        const wanted = new URLSearchParams(location.search).get('borrow');
        if (wanted !== null && user) borrowForm.item_name = wanted;
    });

    $: if (ready) enter(currentView);

    function enter(view) {
        if (view === 'borrow') {
            if (!user) {
                location.href = loginUrl();
                return;
            }
            showCustomDuration = false;
            loadItems();
        } else if (view === 'return') {
            loadActiveLoans();
        } else if (view === 'printer-guide') {
            showWifiPassword = false;
        } else {
            loadMyLoans();
        }
    }

    function go(view) {
        message = '';
        pushState(`?view=${view}`, { view, pushed: true });
        window.scrollTo(0, 0);
    }

    function goHome() {
        // Step back if we got here by a tap, so history stays clean
        if ($page.state.pushed) history.back();
        else replaceState('/', { view: 'home' });
    }

    // State management
    let items = [];
    let loans = [];
    let myLoans = [];
    let loading = false;
    let isSubmitting = false;
    let message = '';
    let messageType = ''; // 'success' or 'error'

    $: openMine = myLoans.filter((l) => l.status === 'active' || l.status === 'pending');

    // Last purpose chip, so regulars tap even less
    const PURPOSE_KEY = 'rrc_purpose';
    function savedPurpose() {
        try {
            return localStorage.getItem(PURPOSE_KEY) || '';
        } catch {
            return '';
        }
    }

    function pickPurpose(preset) {
        borrowForm.purpose = borrowForm.purpose === preset ? '' : preset;
        try {
            localStorage.setItem(PURPOSE_KEY, borrowForm.purpose);
        } catch {
            // storage blocked: just don't remember
        }
    }

    // Borrow form data
    let borrowForm = newBorrowForm();

    function newBorrowForm() {
        return {
            item_name: '',
            lab_location: '',
            quantity_borrowed: 1,
            due_date: dateIn(1),
            purpose: browser ? savedPurpose() : '',
            accept: false
        };
    }

    // Same rule as the server: case, spaces and punctuation don't matter
    const itemKey = (name) => (name || '').toLowerCase().replace(/[^\p{L}\p{N}]/gu, '');
    // The catalogue item the typed name matches, if any; otherwise it's new
    $: selectedItem = items.find((i) => itemKey(i.name) === itemKey(borrowForm.item_name) && itemKey(i.name) !== '');
    $: holders = selectedItem ? selectedItem.out.filter((l) => l.status === 'active') : [];

    // The backend stores only a date and an item is due at the end of it,
    // so "Today" means back by tonight.
    const durationPresets = [
        { label: 'Today', days: 0 },
        { label: 'Tomorrow', days: 1 },
        { label: '3 days', days: 3 },
        { label: '1 week', days: 7 }
    ];
    let showCustomDuration = false;

    // Common purposes, so most people never have to type one
    const purposePresets = ['Project work', 'Course assignment', 'Research', 'Testing'];

    // Lab locations
    const labLocations = [
        { value: 'Main Lab', label: 'Main Lab' },
        { value: 'Mech Lab', label: 'Mech Lab' },
        { value: 'Control Lab', label: 'Control Lab' }
    ];

    // The printer wifi password is hidden behind a click so it is not just
    // sitting on screen for anyone glancing over
    let showWifiPassword = false;

    // Return search
    let searchQuery = '';
    let filteredLoans = [];

    // Returning asks for the phone number the item was borrowed with, so
    // only the borrower can mark it back. One card is open at a time.
    let returningLoanId = null;
    let returnPhone = '';
    let returnError = '';

    $: updateFilteredLoans(loans, searchQuery, user);

    function updateFilteredLoans(currentLoans, currentSearchQuery, me) {
        if (!currentLoans) {
            filteredLoans = [];
            return;
        }

        let loansToFilter = [...currentLoans];

        if (currentSearchQuery && currentSearchQuery.trim()) {
            const query = currentSearchQuery.toLowerCase().trim();
            loansToFilter = currentLoans.filter(loan => {
                const borrowerName = (loan.borrower_name || '').toLowerCase();
                const itemName = (loan.item_name || '').toLowerCase();
                return borrowerName.includes(query) || itemName.includes(query);
            });
        }

        // Your own loans first, then Missing -> Borrowed -> Returned
        const mine = (loan) => (me && loan.user_id === me.id ? 0 : 1);
        const priority = (loan) => ({ not_found: 1, active: 2, returned: 3 })[loan.status] || 4;
        filteredLoans = loansToFilter.sort((a, b) =>
            mine(a) - mine(b) ||
            priority(a) - priority(b) ||
            new Date(b.CreatedAt) - new Date(a.CreatedAt)
        );
    }

    // Clear search function
    function clearSearch(event) {
        if (event) {
            event.preventDefault();
            event.stopPropagation();
        }
        searchQuery = '';
        // Focus back on the search input after clearing
        setTimeout(() => {
            const searchInput = document.getElementById('search');
            if (searchInput) {
                searchInput.focus();
            }
        }, 50);
    }

    // Phone saved on this device by the old borrow form; loans made before
    // accounts are still returned with it.
    const CONTACT_KEY = 'rrc_contact';

    // Load items for borrowing
    async function loadItems() {
        try {
            const response = await fetch('/api/items');
            if (response.ok) {
                items = await response.json();
            } else {
                showMessage('Could not load the item list. Please refresh the page.', 'error');
            }
        } catch (e) {
            console.error('Failed to load items:', e);
            showMessage('Could not load the item list. Check your connection and refresh.', 'error');
        }
    }

    // Load active loans for returning
    async function loadActiveLoans() {
        loading = true;
        try {
            const response = await fetch('/api/loans/active');
            if (response.ok) {
                loans = await response.json();
            }
        } catch (e) {
            showMessage('Failed to load loans', 'error');
        } finally {
            loading = false;
        }
    }

    // What the logged-in person has out, for the strip on the home view
    async function loadMyLoans() {
        if (!user) {
            myLoans = [];
            return;
        }
        const response = await api('/api/loans/mine').catch(() => null);
        if (response?.ok) myLoans = await response.json();
    }

    // Submit a borrow. Who is borrowing comes from the account.
    async function submitBorrow(confirmOver = false) {
        if (!user) {
            location.href = loginUrl();
            return;
        }
        isSubmitting = true;
        try {
            const formData = new FormData();
            if (selectedItem) {
                formData.append('item_id', String(selectedItem.ID));
            } else {
                formData.append('item_name', borrowForm.item_name);
                formData.append('lab_location', borrowForm.lab_location);
            }
            formData.append('quantity_borrowed', borrowForm.quantity_borrowed.toString());
            formData.append('expected_return_date', borrowForm.due_date);
            formData.append('purpose', borrowForm.purpose);
            formData.append('accept', String(borrowForm.accept));
            formData.append('confirm_over', String(confirmOver));

            const response = await api('/api/borrow', { method: 'POST', body: formData });
            const result = await response.json().catch(() => ({}));

            if (response.ok) {
                borrowForm = newBorrowForm();
                goHome();
                showMessage(result.message || 'Item borrowed successfully!', 'success');
            } else if (response.status === 409 && result.confirm_over) {
                isSubmitting = false;
                if (confirm(result.error)) await submitBorrow(true);
                return;
            } else if (response.status === 401) {
                location.href = loginUrl();
            } else {
                showMessage(result.error || 'Failed to borrow', 'error');
            }
        } catch (error) {
            console.error('Error submitting borrow request:', error);
            showMessage('Network error. Please try again.', 'error');
        } finally {
            isSubmitting = false;
        }
    }

    // One tap for your own loans: the account proves who you are. For a
    // loan still waiting for approval this cancels the request.
    async function returnMine(loan) {
        loading = true;
        try {
            const response = await api(`/api/return/${loan.ID}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
            if (response.status === 401) {
                location.href = loginUrl();
                return;
            }
            const result = await response.json().catch(() => ({}));
            showMessage(result.message || result.error || `HTTP ${response.status}`, response.ok ? 'success' : 'error');
            if (currentView === 'return') loadActiveLoans();
            loadMyLoans();
        } catch {
            showMessage('Network error. Please try again.', 'error');
        } finally {
            loading = false;
        }
    }

    // The gear went straight to you from whoever had it
    async function takeOver(loan) {
        if (!user) {
            location.href = loginUrl();
            return;
        }
        if (!confirm(`Take ${loan.item_name} over from ${loan.borrower_name}? It moves to your account and you're responsible for it.`)) return;
        const response = await api(`/api/loans/${loan.ID}/handover`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ accept: true })
        }).catch(() => null);
        if (!response) {
            showMessage('Network error. Please try again.', 'error');
            return;
        }
        if (response.status === 401) {
            location.href = loginUrl();
            return;
        }
        const result = await response.json().catch(() => ({}));
        showMessage(result.message || result.error || `HTTP ${response.status}`, response.ok ? 'success' : 'error');
        loadActiveLoans();
    }

    // Open the phone check for one loan card
    function startReturn(loanId) {
        returningLoanId = loanId;
        // Most people return from the device they borrowed on
        returnPhone = returnPhone || user?.phone || savedPhone();
        returnError = '';
    }

    function cancelReturn() {
        returningLoanId = null;
        returnError = '';
    }

    function savedPhone() {
        try {
            const saved = JSON.parse(localStorage.getItem(CONTACT_KEY) || '{}');
            return (saved && saved.phone) || '';
        } catch (e) {
            return '';
        }
    }

    // Return an item
    async function returnItem(loanId) {
        if (!returnPhone.trim()) {
            returnError = 'Enter the phone number you borrowed this item with.';
            return;
        }
        loading = true;
        returnError = '';
        try {
            const response = await fetch(`/api/return/${loanId}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ phone: returnPhone.trim() })
            });

            if (response.ok) {
                const result = await response.json();
                returningLoanId = null;
                showMessage(result.message, 'success');
                loadActiveLoans(); // Refresh the list
            } else {
                const error = await response.json().catch(() => ({}));
                if (response.status === 403) {
                    returnError = 'That phone number does not match the one this item was borrowed with.';
                } else {
                    returnError = error.error || 'Failed to return item';
                    if (response.status === 404 || response.status === 409) {
                        loadActiveLoans();
                    }
                }
            }
        } catch (e) {
            returnError = 'Network error. Please try again.';
        } finally {
            loading = false;
        }
    }

    // Helper functions
    let messageTimer;
    function showMessage(text, type) {
        message = text;
        messageType = type;
        clearTimeout(messageTimer);
        messageTimer = setTimeout(() => {
            message = '';
            messageType = '';
        }, 5000);
    }

    // YYYY-MM-DD for a date in the browser's own timezone. toISOString()
    // gives the UTC date, which is a day early here for a few hours each night.
    function localDateString(date) {
        const pad = (n) => String(n).padStart(2, '0');
        return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
    }

    // The local date `days` from today
    function dateIn(days) {
        const d = new Date();
        d.setDate(d.getDate() + days);
        return localDateString(d);
    }

    // A plain YYYY-MM-DD means that day here, not midnight UTC
    function parseDay(returnDate) {
        const plain = /^(\d{4})-(\d{2})-(\d{2})/.exec(returnDate || '');
        return plain ? new Date(+plain[1], +plain[2] - 1, +plain[3]) : null;
    }

    // "Tue 21 Oct"
    function shortDay(returnDate) {
        const day = parseDay(returnDate);
        return day ? day.toLocaleDateString('en-GB', { weekday: 'short', day: 'numeric', month: 'short' }) : returnDate;
    }

    // An item is due at the END of its return date, so something due today is
    // not late yet. Matches the backend and the admin sort order.
    function dueDeadline(returnDate) {
        const due = parseDay(returnDate) || new Date(returnDate);
        if (isNaN(due)) return null;
        due.setHours(23, 59, 59, 999);
        return due;
    }

    // "due tomorrow", "due Tue 21 Oct", "waiting for approval", "overdue"
    function dueText(loan) {
        if (loan.status === 'pending') return 'waiting for approval';
        if (loan.overdue) return loan.days_overdue ? `overdue by ${loan.days_overdue} day${loan.days_overdue === 1 ? '' : 's'}` : 'overdue';
        const day = parseDay(loan.expected_return_date);
        if (!day) return '';
        const days = Math.round((day - parseDay(dateIn(0))) / 864e5);
        if (days <= 0) return 'due today';
        if (days === 1) return 'due tomorrow';
        return `due ${shortDay(loan.expected_return_date)}`;
    }

    function formatDate(dateString) {
        return new Date(dateString).toLocaleDateString();
    }

    function formatExpectedReturn(dateString) {
        const expectedDate = dueDeadline(dateString) || new Date(dateString);
        const now = new Date();
        const diffMs = expectedDate.getTime() - now.getTime();
        const diffHours = Math.ceil(diffMs / (1000 * 60 * 60));
        const diffDays = Math.ceil(diffMs / (1000 * 60 * 60 * 24));

        if (diffMs < 0) {
            return `Overdue (${formatDate(dateString)})`;
        } else if (diffHours <= 24) {
            return `${diffHours} hour${diffHours !== 1 ? 's' : ''} (${expectedDate.toLocaleString()})`;
        } else {
            return `${diffDays} day${diffDays !== 1 ? 's' : ''} (${expectedDate.toLocaleString()})`;
        }
    }
</script>

<div class="page">
    {#if message}
        <div class="message {messageType}" role="status">
            {message}
        </div>
    {/if}

    {#if currentView === 'home'}
        <header class="page-head">
            <div>
                <h1>Lab equipment</h1>
                <p>Borrow and return gear, book robots, check the printers.</p>
            </div>
        </header>

        {#if openMine.length}
            <section class="card hued mine" style="--hue: {AREA.equipment}" aria-labelledby="my-loans-title">
                <p class="label" id="my-loans-title">You have</p>
                {#each openMine as loan (loan.ID)}
                    <div class="row">
                        <span class="grow">
                            <strong>{loan.item_name}</strong>{#if loan.quantity_borrowed > 1}<span class="mono"> ×{loan.quantity_borrowed}</span>{/if}
                            <span class="due" class:late={loan.overdue} class:waiting={loan.status === 'pending'}>{dueText(loan)}</span>
                        </span>
                        <button class="btn" on:click={() => returnMine(loan)} disabled={loading}>
                            {loan.status === 'pending' ? 'Cancel request' : 'Returned'}
                        </button>
                    </div>
                {/each}
            </section>
        {/if}

        <div class="grid tiles">
            <button class="card tile main" on:click={() => go('borrow')} style="--hue: {AREA.equipment}">
                <span class="icon-badge"><Icon name="box" size={20} /></span>
                <span class="tile-title">Borrow</span>
                <span class="muted">Take equipment out of the lab</span>
            </button>
            <button class="card tile main" on:click={() => go('return')} style="--hue: {AREA.home}">
                <span class="icon-badge"><Icon name="arrow" size={20} /></span>
                <span class="tile-title">Return</span>
                <span class="muted">Give equipment back</span>
            </button>
            <a class="card tile" href="/mechlab" style="--hue: {AREA.mech}">
                <span class="icon-badge"><Icon name="bot" size={20} /></span>
                <span class="tile-title">Mech Lab</span>
                <span class="muted">Book a manipulator</span>
            </a>
            <a class="card tile" href="/mocap" style="--hue: {AREA.mocap}">
                <span class="icon-badge"><Icon name="video" size={20} /></span>
                <span class="tile-title">Motion capture</span>
                <span class="muted">Book a slot, see the calendar</span>
            </a>
            <a class="card tile" href="/printers" style="--hue: {AREA.printers}">
                <span class="icon-badge"><Icon name="printer" size={20} /></span>
                <span class="tile-title">3D printers</span>
                <span class="muted">See which printers are free</span>
            </a>
            <a class="card tile" href="/items" style="--hue: {AREA.equipment}">
                <span class="icon-badge"><Icon name="grid" size={20} /></span>
                <span class="tile-title">What's in the lab</span>
                <span class="muted">Who has what, report a problem</span>
            </a>
        </div>
    {/if}

    {#if currentView === 'borrow'}
        <header class="page-head">
            <div>
                <h1>Borrow equipment</h1>
                <p>Borrowing as {user?.name}{#if user?.phone} · <span class="mono">{user.phone}</span>{/if}</p>
            </div>
            <button class="btn" on:click={goHome}>Back</button>
        </header>

        <form class="card form" style="--hue: {AREA.equipment}" on:submit|preventDefault={() => submitBorrow()}>
            <div class="field">
                <label for="item">Item</label>
                <input
                    type="text"
                    id="item"
                    list="catalogue"
                    bind:value={borrowForm.item_name}
                    required
                    autocomplete="off"
                    placeholder="Start typing, e.g. RealSense"
                />
                <datalist id="catalogue">
                    {#each items as item (item.ID)}
                        <option value={item.name}>{item.home_lab}{item.out.length ? ` · ${item.out.length} out` : ''}</option>
                    {/each}
                </datalist>
                {#if selectedItem}
                    <small class="hint">
                        {selectedItem.home_lab}{selectedItem.notes ? ` · ${selectedItem.notes}` : ''}
                        {#if holders.length}· Out with {holders.map((l) => l.borrower_name).join(', ')}{/if}
                    </small>
                    {#if selectedItem.issue}
                        <p class="note warn">Reported problem: {selectedItem.issue}</p>
                    {/if}
                    {#if selectedItem.lab_only}
                        <p class="note warn">Lab only. Please don't take this out of the lab.</p>
                    {/if}
                    {#if selectedItem.needs_approval && !can(user, 'auto_approve')}
                        <p class="note warn">Needs a lab admin's approval. You'll get a notification.</p>
                    {/if}
                    {#if selectedItem.tracked}
                        <label class="accept">
                            <input type="checkbox" bind:checked={borrowForm.accept} required />
                            I take responsibility for this {selectedItem.name} until I return it or hand it on.
                        </label>
                    {/if}
                {:else if itemKey(borrowForm.item_name)}
                    <small class="hint">New item. It'll be added to the catalogue. Which lab does it live in?</small>
                    <select id="lab" bind:value={borrowForm.lab_location} required aria-label="Lab the new item lives in">
                        <option value="">Select the lab</option>
                        {#each labLocations as lab}
                            <option value={lab.value}>{lab.label}</option>
                        {/each}
                    </select>
                {/if}
            </div>

            <div class="field">
                <label for="quantity">Quantity</label>
                <input type="number" id="quantity" class="mono qty" bind:value={borrowForm.quantity_borrowed} min="1" required />
            </div>

            <fieldset class="field">
                <legend>Bring it back</legend>
                <div class="picks">
                    {#each durationPresets as preset}
                        <button
                            type="button"
                            class="chip pick"
                            class:selected={!showCustomDuration && borrowForm.due_date === dateIn(preset.days)}
                            on:click={() => { borrowForm.due_date = dateIn(preset.days); showCustomDuration = false; }}
                        >
                            {preset.label}
                        </button>
                    {/each}
                    <button type="button" class="chip pick" class:selected={showCustomDuration} on:click={() => (showCustomDuration = !showCustomDuration)}>
                        Pick a date
                    </button>
                </div>
                {#if showCustomDuration}
                    <input
                        type="date"
                        aria-label="Return date"
                        bind:value={borrowForm.due_date}
                        min={dateIn(0)}
                        max={dateIn(365)}
                        required
                    />
                {/if}
                <small class="hint">Due back <span class="mono">{shortDay(borrowForm.due_date)}</span></small>
            </fieldset>

            <div class="field">
                <label for="purpose">Purpose <span class="faint">optional</span></label>
                <div class="picks">
                    {#each purposePresets as preset}
                        <button type="button" class="chip pick" class:selected={borrowForm.purpose === preset} on:click={() => pickPurpose(preset)}>
                            {preset}
                        </button>
                    {/each}
                </div>
                <textarea id="purpose" bind:value={borrowForm.purpose} placeholder="Anything more specific? (project name, priority)" rows="2"></textarea>
            </div>

            <button type="submit" class="btn primary submit" disabled={isSubmitting}>
                {isSubmitting ? 'Saving…' : 'Borrow'}
            </button>
        </form>
    {/if}

    {#if currentView === 'return'}
        <header class="page-head">
            <div>
                <h1>Return equipment</h1>
                <p>Your own loans are at the top.</p>
            </div>
            <button class="btn" on:click={goHome}>Back</button>
        </header>

        <div class="field search">
            <label for="search">Search by name or item</label>
            <div class="search-row">
                <input type="text" id="search" bind:value={searchQuery} placeholder="Name or item" autocomplete="off" />
                {#if searchQuery && searchQuery.trim()}
                    <button class="btn" on:click={clearSearch} type="button">Clear</button>
                {/if}
            </div>
        </div>

        {#if loading}
            <p class="muted">Loading…</p>
        {:else if filteredLoans.length === 0}
            <div class="card">
                <p class="label">No items found</p>
                <p class="muted flush">
                    {searchQuery.trim() ? 'Try different words, or clear the search to see everything.' : 'Nothing is out right now.'}
                </p>
            </div>
        {:else}
            <p class="label"><span class="mono">{filteredLoans.length}</span> {searchQuery.trim() ? 'found' : 'items'}</p>
            <div class="grid loans">
                {#each filteredLoans as loan (loan.ID)}
                    {@const itemCategory = loan.status === 'not_found' ? 'missing' : loan.status === 'returned' ? 'returned' : 'borrowed'}
                    <article class="card">
                        <div class="split">
                            <h2>{loan.item_name}</h2>
                            {#if itemCategory === 'missing'}
                                <span class="chip warn">Missing</span>
                            {:else if itemCategory === 'returned'}
                                <span class="chip ok">Returned</span>
                            {:else}
                                <span class="chip hue" style="--hue: {AREA.equipment}">Borrowed</span>
                            {/if}
                        </div>
                        <div class="row"><span class="muted">Borrower</span><span>{loan.borrower_name}</span></div>
                        <div class="row"><span class="muted">Lab</span><span>{loan.lab_location}</span></div>
                        <div class="row"><span class="muted">Quantity</span><span class="mono">{loan.quantity_borrowed}</span></div>
                        <div class="row"><span class="muted">Borrowed</span><span class="mono">{formatDate(loan.CreatedAt)}</span></div>
                        {#if loan.expected_return_date}
                            {@const due = formatExpectedReturn(loan.expected_return_date)}
                            <div class="row"><span class="muted">Due</span><span class="mono end" class:late={due.startsWith('Overdue')}>{due}</span></div>
                        {/if}

                        {#if loan.photo_filename}
                            <img class="photo" src="/api/photos/{loan.photo_filename}" alt={loan.item_name} loading="lazy" />
                        {/if}

                        <div class="actions">
                            {#if itemCategory === 'missing'}
                                <p class="note warn">Marked missing by an admin. If you find it, tell lab staff.</p>
                            {:else if itemCategory === 'borrowed' && loan.user_id && user && loan.user_id === user.id}
                                <button class="btn" on:click={() => returnMine(loan)} disabled={loading}>Returned</button>
                            {:else if itemCategory === 'borrowed' && loan.user_id}
                                <button class="btn" on:click={() => takeOver(loan)} disabled={loading}>I have this now</button>
                            {:else if itemCategory === 'borrowed'}
                                {#if returningLoanId === loan.ID}
                                    <form class="confirm" on:submit|preventDefault={() => returnItem(loan.ID)}>
                                        <label for="return-phone-{loan.ID}">Phone number you borrowed it with</label>
                                        <input
                                            id="return-phone-{loan.ID}"
                                            type="tel"
                                            inputmode="numeric"
                                            autocomplete="tel"
                                            bind:value={returnPhone}
                                            placeholder="10-digit phone number"
                                        />
                                        {#if returnError}
                                            <p class="note late" role="alert">{returnError}</p>
                                        {/if}
                                        <div class="buttons">
                                            <button type="submit" class="btn primary" disabled={loading}>Confirm return</button>
                                            <button type="button" class="btn" on:click={cancelReturn}>Cancel</button>
                                        </div>
                                    </form>
                                {:else}
                                    <button class="btn" on:click={() => startReturn(loan.ID)} disabled={loading}>Mark as returned</button>
                                {/if}
                            {/if}
                        </div>
                    </article>
                {/each}
            </div>
        {/if}
    {/if}

    {#if currentView === 'printer-guide'}
        <header class="page-head">
            <div>
                <h1>Printer guide</h1>
                <p>How to use the Bambu Lab P1S printers.</p>
            </div>
            <button class="btn" on:click={goHome}>Back</button>
        </header>

        <article class="card guide">
            <!-- How to print, in the order you actually do it -->
            <section>
                <h2>How to print</h2>

                <div class="callout">
                    <strong>The one rule: put your name in the file name.</strong>
                    <p>
                        Save your sliced file as <code>yourname_object.3mf</code> - for example
                        <code>srinath_bracket.3mf</code>. The printer reports the file name, so the
                        <a href="/printers">printer page</a> shows whose print is running on each
                        machine. A print nobody can identify is a print that gets stopped when
                        someone else needs the printer.
                    </p>
                </div>

                <ol>
                    <li>
                        <strong>Get on the printer network.</strong>
                        The printers have their own network inside the lab - they are not
                        reachable from wifi@iiith.
                        <div class="wifi">
                            <div class="row">
                                <span class="muted">Network</span>
                                <code>printers@rrc</code>
                            </div>
                            <div class="row">
                                <span class="muted">Password</span>
                                <span class="reveal">
                                    {#if showWifiPassword}
                                        <code>printers@1234</code>
                                        <button class="btn" on:click={() => (showWifiPassword = false)}>Hide</button>
                                    {:else}
                                        <code>••••••••••••</code>
                                        <button class="btn" on:click={() => (showWifiPassword = true)}>Show</button>
                                    {/if}
                                </span>
                            </div>
                            <p class="hint">Lab members only. Do not post this in group chats or share it outside the lab.</p>
                        </div>
                    </li>
                    <li>
                        <strong>Check a printer is actually free.</strong>
                        Open the <a href="/printers">printer page</a>. It shows Free or In use,
                        progress and time remaining for all three printers.
                        <em>Look at the camera before you walk over</em> - a printer can say
                        "Finished" while someone's finished print is still sitting on the plate.
                        The plate must be empty and clean before you start.
                    </li>
                    <li>
                        <strong>Install Bambu Studio.</strong>
                        You need it to slice your model - it converts your STL into
                        instructions the printer understands. Download it from
                        <a href="https://bambulab.com/en/download/studio" target="_blank" rel="noopener noreferrer">bambulab.com</a>.
                    </li>
                    <li>
                        <strong>Add the printer in Bambu Studio</strong> (first time only).
                        Use LAN mode and enter the printer's IP and access code, both shown on
                        the printer's own screen under Settings → Network.
                        <div class="table-wrap">
                            <table>
                                <thead>
                                    <tr><th>Printer</th><th>Label on the machine</th><th>IP address</th></tr>
                                </thead>
                                <tbody>
                                    <tr><td>Printer 1</td><td class="mono">3DP-01P-279</td><td><code>192.168.2.101</code></td></tr>
                                    <tr><td>Printer 2</td><td class="mono">3DP-01P-112</td><td><code>192.168.2.105</code></td></tr>
                                    <tr><td>Printer 3</td><td class="mono">3DP-01P-739</td><td><code>192.168.2.102</code></td></tr>
                                </tbody>
                            </table>
                        </div>
                        <span class="hint">
                            Access codes are on each printer's screen. They are not listed here on
                            purpose - if a code stops working, tell an admin.
                        </span>
                    </li>
                    <li>
                        <strong>Slice with the right printer and the right material.</strong>
                        Select <em>Bambu Lab P1S</em>, and set the filament profile to the
                        material that is <em>actually loaded</em> in that machine.
                        <span class="note late">
                            Printing PLA with PETG settings (or the other way round) melts at the
                            wrong temperature and <strong>clogs the hotend</strong>. A clog takes
                            the printer out of service and is a real repair job - check twice.
                        </span>
                    </li>
                    <li>
                        <strong>Get your settings checked.</strong>
                        Before your first few prints, show your slicer settings to a lab admin.
                        Thirty seconds of checking saves a failed 10 hour print and wasted
                        filament.
                    </li>
                    <li>
                        <strong>Check the plate and the filament before you start.</strong>
                        The plate must be empty, clean and free of leftover plastic - anything
                        stuck on it ruins the first layer. Confirm filament is loaded properly and
                        is the material you sliced for, and that there is enough left on the spool
                        for your print.
                    </li>
                    <li>
                        <strong>Name the file with your name</strong> (see the rule above), then
                        send it. Two ways:
                        <ul>
                            <li>
                                <strong>From this website</strong> (easiest, no wifi switching):
                                open <a href="/printers">3D printers</a>, pick the machine,
                                <em>Send a file to print</em>, and drop your exported file in.
                                Then walk over, check the plate, and start it from the printer's
                                screen.
                            </li>
                            <li>
                                <strong>From Bambu Studio</strong>, if you are already on the
                                printer network.
                            </li>
                        </ul>
                    </li>
                    <li>
                        <strong>Watch the first layer, then check in regularly.</strong>
                        Stay until the first layer is down - most failures happen there. After
                        that keep checking on it every so often from the
                        <a href="/printers">printer page</a>; the camera shows you whether it is
                        still printing properly. Do not start a print and forget about it.
                    </li>
                    <li>
                        <strong>Collect your print promptly and clear the plate.</strong>
                        The next person cannot start until the bed is empty. Take your waste
                        (purge blobs, skirts, supports) with you.
                    </li>
                </ol>
            </section>

            <section>
                <h2>Loading and unloading filament</h2>
                <p>
                    Always use the printer's own screen menu. Never pull filament out cold and
                    never force it - that is how hotends get damaged.
                </p>
                <div class="two-col">
                    <div>
                        <h3>Loading</h3>
                        <ol>
                            <li>Put the spool on the holder so it feeds without tangling.</li>
                            <li>Snip the end at an angle so it feeds in cleanly.</li>
                            <li>Push it through the PTFE tube until it reaches the extruder.</li>
                            <li>On the screen, choose <em>Filament → Load</em> and pick the material.</li>
                            <li>Wait for the nozzle to heat and extrude, until the colour coming
                                out is only your new filament.</li>
                        </ol>
                    </div>
                    <div>
                        <h3>Unloading</h3>
                        <ol>
                            <li>On the screen, choose <em>Filament → Unload</em>.</li>
                            <li>Wait for the nozzle to heat - this is not optional.</li>
                            <li>Let the printer retract it, then pull gently. If it resists,
                                stop and ask an admin.</li>
                            <li>Clip the end and secure it on the spool so it does not tangle
                                for the next person.</li>
                        </ol>
                    </div>
                </div>
                <p class="hint">Detailed official instructions are in the Bambu Lab documentation linked below.</p>
            </section>

            <section>
                <h2>Questions about the printer page</h2>
                <ul>
                    <li>
                        <strong>Your print is failing and you are not in the lab?</strong>
                        Message a lab admin - admins can stop any print remotely from the
                        <a href="/printers">printer page</a>. Faults themselves are covered in
                        the safety section below.
                    </li>
                    <li>
                        <strong>A printer shows Offline?</strong> It is switched off or off the
                        network. Tell an admin.
                    </li>
                    <li>
                        <strong>Your print stopped and you do not know why?</strong> An admin may
                        have stopped it - the printer page shows who did. Ask them.
                    </li>
                    <li>
                        <strong>A printer says "Access code changed"?</strong> Nothing you did.
                        An admin needs to update it; tell them.
                    </li>
                </ul>
            </section>

            <section>
                <h2>Safety</h2>
                <ul>
                    <li>
                        <strong>Be careful with the scraper.</strong> The blade is sharp and
                        prints come loose suddenly. Scrape <em>away</em> from your hand, never
                        towards it, and take the plate off the printer first. More people are
                        hurt by the scraper than by anything else on these machines.
                    </li>
                    <li>
                        <strong>The nozzle and bed are hot</strong> - well over 200 °C at the
                        nozzle. Let things cool before reaching inside.
                    </li>
                    <li>
                        <strong>Keep the lid closed while printing</strong>, and do not reach in
                        to "fix" something mid-print. Pause or stop it first.
                    </li>
                </ul>
            </section>

            <section>
                <h2>Lab rules</h2>
                <ul>
                    <li>Put your name in the file name. Every time.</li>
                    <li>
                        <strong>Filament that is not yours needs permission.</strong> Ask the
                        owner or a lab admin before using someone else's spool. If you are
                        unsure whose it is, ask - do not assume it is lab stock.
                    </li>
                    <li>Do not start a long print and disappear without telling anyone.</li>
                    <li>Do not cancel or remove someone else's print. Ask an admin.</li>
                    <li>Do not change printer settings, calibration or firmware.</li>
                    <li>
                        <strong>Keep the printer area clean.</strong> Clear away purge blobs,
                        supports and offcuts, put tools back, and leave the bench tidy for the
                        next person.
                    </li>
                    <li>If you break something, say so. Hiding it costs the lab more.</li>
                </ul>
            </section>

            <section>
                <h2>Official documentation</h2>
                <a class="row link-row" href="https://wiki.bambulab.com/en/p1/manual/loading-filament" target="_blank" rel="noopener noreferrer">
                    <span><strong>Loading filament</strong><span class="hint">Step-by-step guide to load filament into the printer</span></span>
                    <Icon name="arrow" />
                </a>
                <a class="row link-row" href="https://wiki.bambulab.com/en/p1/manual" target="_blank" rel="noopener noreferrer">
                    <span><strong>Replacing filament</strong><span class="hint">How to change and replace filament during printing</span></span>
                    <Icon name="arrow" />
                </a>
                <a class="row link-row" href="https://wiki.bambulab.com/en/p1/manual/print-from-bambu-studio" target="_blank" rel="noopener noreferrer">
                    <span><strong>Sending print jobs</strong><span class="hint">How to send print jobs from Bambu Studio</span></span>
                    <Icon name="arrow" />
                </a>
                <a class="row link-row" href="https://wiki.bambulab.com/en/general/filament-guide-material-table" target="_blank" rel="noopener noreferrer">
                    <span><strong>Filament guide</strong><span class="hint">Complete material table and filament reference</span></span>
                    <Icon name="arrow" />
                </a>
            </section>

            <section>
                <h2>When to stop a print</h2>
                <p>If you notice any of these during a print, <strong>stop the print immediately</strong> and tell lab staff.</p>

                <h3>Nozzle blockage</h3>
                <p>The nozzle appears blocked or no filament is extruding.</p>
                <ul>
                    <li>Stop the print immediately.</li>
                    <li>Do not try to clear it yourself.</li>
                    <li>Tell lab staff right away.</li>
                </ul>

                <h3>Tangled filament or printing in air</h3>
                <p>The filament is tangled or the printer is printing in mid-air.</p>
                <ul>
                    <li>Stop the print immediately.</li>
                    <li>Do not touch the hot nozzle or bed.</li>
                    <li>Report the issue to lab staff.</li>
                </ul>

                <h3>Failed or messed up print</h3>
                <p>The print quality is poor or the print is failing.</p>
                <ul>
                    <li>Stop the print to avoid wasting material.</li>
                    <li>Take a photo of the issue if you can.</li>
                    <li>Ask lab staff for help.</li>
                </ul>

                <h3>Anyone can stop a bad print</h3>
                <ul>
                    <li>If you notice a problem on any print, act on it.</li>
                    <li>Keeping the equipment working is everyone's job.</li>
                    <li>Tell lab staff about prints you stopped.</li>
                    <li>Better to stop early than damage the printer.</li>
                </ul>
            </section>

            <section>
                <h2>Need help?</h2>
                <p>If something goes wrong or you have questions, contact lab staff. It is always better to stop a print and ask than to damage the equipment.</p>
            </section>
        </article>
    {/if}
</div>

<style>
    /* Home */
    .mine {
        margin-bottom: 14px;
    }

    .grow {
        min-width: 0;
    }

    .due {
        display: block;
        font-size: 0.9rem;
        color: var(--muted);
    }

    .late {
        color: var(--danger);
    }

    .waiting {
        color: var(--warn);
    }

    .tiles {
        grid-template-columns: repeat(4, minmax(0, 1fr));
    }

    .tile {
        display: flex;
        flex-direction: column;
        align-items: flex-start;
        gap: 4px;
        font: inherit;
        color: inherit;
        text-align: left;
        text-decoration: none;
        cursor: pointer;
        transition: border-color var(--fast) var(--ease);
    }

    .tile:hover {
        border-color: color-mix(in srgb, var(--hue) 45%, var(--line));
    }

    .tile.main {
        grid-column: span 2;
    }

    .tile .icon-badge {
        margin-bottom: 14px;
    }

    .tile-title {
        font-size: 1.05rem;
        font-weight: 600;
    }

    .tile .muted {
        font-size: 0.9rem;
    }

    /* Forms */
    .form {
        max-width: 640px;
    }

    .field {
        display: flex;
        flex-direction: column;
        gap: 8px;
        margin: 0 0 20px;
        padding: 0;
        border: 0;
        min-width: 0;
    }

    .field > label,
    .field > legend,
    .confirm > label {
        padding: 0;
        font-size: 0.85rem;
        font-weight: 500;
        color: var(--muted);
    }

    .field > legend {
        margin-bottom: 8px;
    }

    input:not([type='checkbox']),
    select,
    textarea {
        box-sizing: border-box;
        width: 100%;
        min-height: 42px;
        padding: 9px 12px;
    }

    textarea {
        resize: vertical;
    }

    input.qty {
        max-width: 120px;
    }

    .hint {
        display: block;
        font-size: 0.85rem;
        color: var(--muted);
    }

    .faint {
        color: var(--faint);
        font-weight: 400;
    }

    .note {
        margin: 0;
        font-size: 0.9rem;
    }

    .note.warn {
        color: var(--warn);
    }

    .accept {
        display: flex;
        gap: 10px;
        align-items: flex-start;
        font-size: 0.9rem;
    }

    .accept input {
        margin-top: 3px;
    }

    .picks {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
    }

    .pick {
        min-height: 36px;
        padding: 0 14px;
        background: transparent;
        font: inherit;
        font-size: 0.9rem;
        cursor: pointer;
    }

    .pick:hover {
        border-color: var(--faint);
    }

    .pick.selected {
        background: color-mix(in srgb, var(--hue) 14%, transparent);
        border-color: color-mix(in srgb, var(--hue) 45%, transparent);
        color: var(--hue);
    }

    /* Return */
    .search {
        max-width: 640px;
    }

    .search-row {
        display: flex;
        gap: 8px;
    }

    .loans {
        grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr));
    }

    .split {
        display: flex;
        align-items: flex-start;
        justify-content: space-between;
        gap: 10px;
        margin-bottom: 6px;
    }

    .split .chip {
        flex: none;
    }

    h2 {
        margin: 0;
        font-size: 1.1rem;
        font-weight: 600;
    }

    .loans .row {
        padding: 9px 0;
        font-size: 0.92rem;
    }

    .end {
        text-align: right;
    }

    .photo {
        display: block;
        max-width: 100%;
        max-height: 180px;
        margin-top: 10px;
        border-radius: var(--radius-sm);
        object-fit: cover;
    }

    .actions {
        margin-top: 12px;
    }

    .confirm {
        display: flex;
        flex-direction: column;
        gap: 8px;
    }

    .buttons {
        display: flex;
        gap: 8px;
    }

    .flush {
        margin: 0;
    }

    /* Printer guide */
    .guide {
        max-width: 760px;
        line-height: 1.6;
    }

    .guide section + section {
        margin-top: 28px;
        padding-top: 24px;
        border-top: 1px solid var(--line);
    }

    .guide h2 {
        margin-bottom: 12px;
    }

    .guide h3 {
        margin: 18px 0 4px;
        font-size: 0.95rem;
        font-weight: 600;
    }

    .guide p {
        margin: 0 0 10px;
    }

    .guide ol,
    .guide ul {
        margin: 0;
        padding-left: 1.3em;
    }

    .guide li + li {
        margin-top: 8px;
    }

    .guide a:not(.row) {
        color: var(--accent);
    }

    .guide code {
        font-family: var(--mono);
        font-size: 0.9em;
    }

    .guide .note {
        display: block;
        margin-top: 6px;
    }

    .callout {
        margin-bottom: 18px;
        padding: 14px 16px;
        border-radius: var(--radius);
        background: var(--accent-soft);
    }

    .callout p {
        margin: 6px 0 0;
    }

    .wifi {
        margin: 10px 0 4px;
        padding: 4px 14px 10px;
        border: 1px solid var(--line);
        border-radius: var(--radius);
    }

    .reveal {
        display: flex;
        align-items: center;
        gap: 10px;
    }

    .table-wrap {
        margin: 10px 0 6px;
        overflow-x: auto;
    }

    table {
        width: 100%;
        border-collapse: collapse;
        font-size: 0.9rem;
    }

    th,
    td {
        padding: 8px 10px 8px 0;
        text-align: left;
        border-bottom: 1px solid var(--line);
        white-space: nowrap;
    }

    th {
        font-weight: 500;
        color: var(--muted);
    }

    .two-col {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
        gap: 8px 24px;
    }

    .link-row {
        color: inherit;
        text-decoration: none;
    }

    .link-row :global(svg) {
        flex: none;
        color: var(--faint);
    }

    .link-row:hover :global(svg) {
        color: var(--accent);
    }

    @media (max-width: 900px) {
        .tiles {
            grid-template-columns: repeat(2, minmax(0, 1fr));
        }

        .tile.main {
            grid-column: auto;
        }
    }

    @media (max-width: 640px) {
        .submit {
            width: 100%;
        }
    }
</style>
