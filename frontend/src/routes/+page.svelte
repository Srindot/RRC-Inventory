<script>
    import { onDestroy, onMount } from 'svelte';
    import { browser } from '$app/environment';
    import { pushState, replaceState } from '$app/navigation';
    import { page } from '$app/stores';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
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
        if (wanted !== null) borrowForm.item_name = wanted;
    });

    onDestroy(() => {
        if (photoPreview) URL.revokeObjectURL(photoPreview);
    });

    $: if (ready) enter(currentView);

    function enter(view) {
        if (view === 'borrow') {
            // No account needed: without one, name and phone are asked for
            // (and remembered on this device, like the old form).
            if (!user) loadSavedContact();
            showCustomDuration = false;
            loadItems();
        } else if (view === 'return') {
            try {
                savedName = (JSON.parse(localStorage.getItem(CONTACT_KEY) || '{}')?.name || '').trim().toLowerCase();
            } catch {
                savedName = '';
            }
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

    function newBorrowForm(prev = {}) {
        return {
            item_name: '',
            lab_location: '',
            quantity_borrowed: 1,
            due_date: dateIn(1),
            purpose: browser ? savedPurpose() : '',
            accept: false,
            item_photo: null,
            // Only sent when not logged in
            borrower_name: prev.borrower_name || '',
            borrower_phone: prev.borrower_phone || ''
        };
    }

    // Name and phone are remembered on this device so repeat borrowers
    // without an account only have to fill in the item.
    function loadSavedContact() {
        try {
            const saved = JSON.parse(localStorage.getItem(CONTACT_KEY) || 'null');
            if (saved) {
                borrowForm.borrower_name = borrowForm.borrower_name || saved.name || '';
                borrowForm.borrower_phone = borrowForm.borrower_phone || saved.phone || '';
            }
        } catch {
            // Unreadable storage: start with an empty form
        }
    }

    function saveContact() {
        try {
            localStorage.setItem(CONTACT_KEY, JSON.stringify({ name: borrowForm.borrower_name.trim(), phone: borrowForm.borrower_phone }));
        } catch {
            // Saving is a convenience - never block a borrow on it
        }
    }

    function forgetContact() {
        try {
            localStorage.removeItem(CONTACT_KEY);
        } catch {
            // Nothing to do
        }
        borrowForm.borrower_name = '';
        borrowForm.borrower_phone = '';
        showMessage('Saved details cleared', 'success');
    }

    // Digits only, at most 10, with the same "exactly 10" check as before
    function onPhoneInput(e) {
        const cleaned = e.target.value.replace(/[^0-9]/g, '').slice(-10);
        borrowForm.borrower_phone = cleaned;
        e.target.value = cleaned;
        e.target.setCustomValidity(cleaned.length === 10 ? '' : 'Please enter exactly 10 digits');
    }

    // Same rule as the server: case, spaces and punctuation don't matter
    const itemKey = (name) => (name || '').toLowerCase().replace(/[^\p{L}\p{N}]/gu, '');
    // The catalogue item the typed name matches, if any; otherwise it's new
    $: selectedItem = items.find((i) => itemKey(i.name) === itemKey(borrowForm.item_name) && itemKey(i.name) !== '');
    $: holders = selectedItem ? selectedItem.out.filter((l) => l.status === 'active') : [];
    $: waiting = selectedItem ? selectedItem.out.filter((l) => l.status === 'pending') : [];
    // How many the lab should have free, when someone has counted them.
    // Same sum as the server: active and pending loans both hold stock.
    $: freeCount =
        selectedItem && selectedItem.total_quantity > 0
            ? Math.max(0, selectedItem.total_quantity - selectedItem.out.reduce((n, l) => n + (l.quantity_borrowed || 0), 0))
            : null;
    $: needsApproval = !!(selectedItem && selectedItem.needs_approval && !can(user, 'auto_approve'));
    // Without an account such an item can't be requested at all
    $: loginToRequest = needsApproval && !user;

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

    // Optional photo of the item as it left the lab. The server sniffs the
    // real type and caps the size too; these checks just answer sooner.
    const MAX_PHOTO_BYTES = 10 * 1024 * 1024;
    const PHOTO_TYPES = ['image/jpeg', 'image/jpg', 'image/png', 'image/webp', 'image/gif', 'image/bmp'];
    let photoInput;
    let photoPreview = '';

    function handlePhotoUpload(event) {
        const file = event.target.files[0];
        if (!file) return;
        if (file.size > MAX_PHOTO_BYTES) {
            showMessage('File size too large. Maximum allowed size is 10MB.', 'error');
            event.target.value = '';
            return;
        }
        if (!PHOTO_TYPES.includes(file.type)) {
            showMessage('Unsupported file format. Please use JPG, PNG, WEBP, GIF or BMP.', 'error');
            event.target.value = '';
            return;
        }
        if (photoPreview) URL.revokeObjectURL(photoPreview);
        photoPreview = URL.createObjectURL(file);
        borrowForm.item_photo = file;
    }

    function clearPhoto() {
        if (photoPreview) URL.revokeObjectURL(photoPreview);
        photoPreview = '';
        borrowForm.item_photo = null;
        if (photoInput) photoInput.value = '';
    }

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

    // Without an account, loans under the name saved on this device are
    // most likely theirs, so they go to the top too.
    let savedName = '';
    $: updateFilteredLoans(loans, searchQuery, user, savedName);

    function updateFilteredLoans(currentLoans, currentSearchQuery, me, myName) {
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
        const mine = (loan) => (isMine(loan) || isProbablyMine(loan) ? 0 : 1);
        const priority = (loan) => ({ not_found: 1, active: 2, returned: 3 })[loan.status] || 4;
        filteredLoans = loansToFilter.sort((a, b) =>
            mine(a) - mine(b) ||
            priority(a) - priority(b) ||
            new Date(b.CreatedAt) - new Date(a.CreatedAt)
        );
    }

    function isMine(loan) {
        return !!(loan.user_id && user && loan.user_id === user.id);
    }

    function isProbablyMine(loan) {
        return !user && !loan.user_id && !!savedName && (loan.borrower_name || '').trim().toLowerCase() === savedName;
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
        if (loginToRequest) {
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
            if (!user) {
                formData.append('borrower_name', borrowForm.borrower_name.trim());
                formData.append('borrower_phone', borrowForm.borrower_phone);
            }
            if (borrowForm.item_photo) {
                formData.append('item_photo', borrowForm.item_photo);
            }

            const response = await api('/api/borrow', { method: 'POST', body: formData });
            const result = await response.json().catch(() => ({}));

            if (response.ok) {
                if (!user) saveContact();
                clearPhoto();
                // Keep the name and phone for next time
                borrowForm = newBorrowForm(borrowForm);
                goHome();
                showMessage(result.message || 'Item borrowed successfully!', 'success');
            } else if (response.status === 409 && result.confirm_over) {
                isSubmitting = false;
                if (confirm(result.error)) await submitBorrow(true);
                return;
            } else if (response.status === 401) {
                // The session ended mid-form: carry on without an account
                user = null;
                loadSavedContact();
                showMessage('Your login has expired. Fill in your name and phone, or log in again.', 'error');
            } else if (response.status === 413) {
                showMessage(result.error || 'That photo is too large. Photos can be up to 10MB.', 'error');
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
        let phone = '';
        if (can(user, 'manage_inventory')) {
            if (!confirm(`Move ${loan.item_name} from ${loan.borrower_name} to your account?`)) return;
        } else {
            // The holder's phone is the proof they handed it over, as for a return.
            phone = prompt(`Take ${loan.item_name} over from ${loan.borrower_name}?\n\nIt moves to your account and you're responsible for it. Enter ${loan.borrower_name}'s phone number to confirm they gave it to you:`);
            if (phone === null) return;
        }
        const response = await api(`/api/loans/${loan.ID}/handover`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ accept: true, phone })
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

    // "today", "tomorrow", "in 3 days" for the chosen return date
    function daysAway(returnDate) {
        const day = parseDay(returnDate);
        if (!day) return '';
        const days = Math.round((day - parseDay(dateIn(0))) / 864e5);
        if (days <= 0) return 'today';
        if (days === 1) return 'tomorrow';
        return `in ${days} days`;
    }

    // An item is due at the END of its return date, so something due today is
    // not late yet. Matches the backend and the admin sort order.
    function dueDeadline(returnDate) {
        const due = parseDay(returnDate) || new Date(returnDate);
        if (isNaN(due)) return null;
        due.setHours(23, 59, 59, 999);
        return due;
    }

    function isOverdue(returnDate) {
        const deadline = dueDeadline(returnDate);
        return deadline !== null && deadline < new Date();
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

        <div class="home-options">
            <h2>What would you like to do?</h2>
            <div class="option-buttons">
                <button class="option-btn" style="--i: 0; --tile-accent: var(--rp-rose)" on:click={() => go('borrow')}>
                    <span class="tile-icon" aria-hidden="true">📦</span>
                    <span class="tile-body">
                        <span class="tile-title">Borrow Item</span>
                        <span class="tile-sub">Take equipment from the lab</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </button>

                <button class="option-btn" style="--i: 1; --tile-accent: var(--rp-foam)" on:click={() => go('return')}>
                    <span class="tile-icon" aria-hidden="true">↩️</span>
                    <span class="tile-body">
                        <span class="tile-title">Return Item</span>
                        <span class="tile-sub">Give equipment back</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </button>

                <a class="option-btn" style="--i: 2; --tile-accent: var(--rp-gold)" href="/printers">
                    <img class="tile-image" src="/P1S.png" alt="" aria-hidden="true" />
                    <span class="tile-body">
                        <span class="tile-title">3D Printers</span>
                        <span class="tile-sub">See which printers are free</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </a>

                <a class="option-btn" style="--i: 3; --tile-accent: var(--rp-iris)" href="/mocap">
                    <span class="tile-icon" aria-hidden="true">🎥</span>
                    <span class="tile-body">
                        <span class="tile-title">Motion Capture Lab</span>
                        <span class="tile-sub">Book a slot, see the calendar</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </a>

                <a class="option-btn" style="--i: 4; --tile-accent: var(--rp-love)" href="/mechlab">
                    <span class="tile-icon" aria-hidden="true">🦾</span>
                    <span class="tile-body">
                        <span class="tile-title">Mech Lab</span>
                        <span class="tile-sub">Book a robot</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </a>

                <a class="option-btn" style="--i: 5; --tile-accent: var(--rp-foam)" href="/items">
                    <span class="tile-icon" aria-hidden="true">🧰</span>
                    <span class="tile-body">
                        <span class="tile-title">What's in the Lab</span>
                        <span class="tile-sub">Who has what, report a problem</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </a>

                <button class="option-btn" style="--i: 6; --tile-accent: var(--rp-gold)" on:click={() => go('printer-guide')}>
                    <span class="tile-icon" aria-hidden="true">📖</span>
                    <span class="tile-body">
                        <span class="tile-title">Printer Guidelines</span>
                        <span class="tile-sub">How to print, safely</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </button>

                {#if user}
                    <a class="option-btn" style="--i: 7; --tile-accent: var(--rp-iris)" href="/dashboard">
                        <span class="tile-icon" aria-hidden="true">📊</span>
                        <span class="tile-body">
                            <span class="tile-title">Dashboard</span>
                            <span class="tile-sub">What needs you, your bookings</span>
                        </span>
                        <span class="tile-arrow" aria-hidden="true">→</span>
                    </a>

                    <a class="option-btn" style="--i: 8; --tile-accent: var(--rp-rose)" href="/threads">
                        <span class="tile-icon" aria-hidden="true">🧪</span>
                        <span class="tile-body">
                            <span class="tile-title">Research Threads</span>
                            <span class="tile-sub">Your projects and robot access</span>
                        </span>
                        <span class="tile-arrow" aria-hidden="true">→</span>
                    </a>
                {/if}
            </div>
        </div>
    {/if}

    <!-- Borrow View -->
    {#if currentView === 'borrow'}
        <div class="form-container">
            <h2 class="view-title borrow-title">📦 Borrow Equipment</h2>
            <p class="view-sub">Pick the item, say when it comes back, and you're done.</p>

            <form on:submit|preventDefault={() => submitBorrow()}>
                {#if user}
                    <div class="form-group">
                        <div class="label-row">
                            <span class="label">Borrowing as</span>
                            <a class="link-btn" href="/account">Not you?</a>
                        </div>
                        <div class="account-box">
                            <span class="account-name">👤 {user.name || ''}</span>
                            {#if user.phone}<span class="account-phone mono">{user.phone}</span>{/if}
                        </div>
                        <small class="help-text">From your account, so you only pick the item and the return date.</small>
                    </div>
                {:else}
                    <div class="login-hint">
                        <span>Have an account? <a href={loginUrl()}>Log in</a> - then you only fill in the item and the date.</span>
                    </div>

                    <div class="form-group">
                        <div class="label-row">
                            <label for="name">Name *</label>
                            {#if borrowForm.borrower_name}
                                <button type="button" class="link-btn" on:click={forgetContact}>Not you?</button>
                            {/if}
                        </div>
                        <input
                            type="text"
                            id="name"
                            bind:value={borrowForm.borrower_name}
                            required
                            maxlength="100"
                            autocomplete="name"
                            placeholder="Enter your name"
                        />
                        <small class="help-text">Your name and phone are saved on this device for next time.</small>
                    </div>

                    <div class="form-group">
                        <label for="phone">Phone Number *</label>
                        <input
                            type="tel"
                            id="phone"
                            value={borrowForm.borrower_phone}
                            required
                            placeholder="Enter 10-digit phone number"
                            title="Please enter exactly 10 digits"
                            inputmode="numeric"
                            autocomplete="tel-national"
                            on:input={onPhoneInput}
                        />
                        <small class="help-text">You'll need this number to return the item.</small>
                    </div>
                {/if}

                <div class="form-group">
                    <label for="item">Item Name *</label>
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
                        <div class="item-info">
                            <div class="info-row">
                                <span class="info-chip">📍 {selectedItem.home_lab || 'Lab not set'}</span>
                                {#if freeCount !== null}
                                    <span class="info-chip" class:short={freeCount < borrowForm.quantity_borrowed}>
                                        {freeCount} of {selectedItem.total_quantity} free
                                    </span>
                                {/if}
                                {#if holders.length}
                                    <span class="info-chip">Out with {holders.map((l) => l.borrower_name).join(', ')}</span>
                                {/if}
                                {#if waiting.length}
                                    <span class="info-chip">{waiting.length} request{waiting.length === 1 ? '' : 's'} waiting</span>
                                {/if}
                            </div>
                            {#if selectedItem.notes}
                                <small class="help-text">{selectedItem.notes}</small>
                            {/if}
                        </div>
                        {#if selectedItem.issue}
                            <p class="status-message pending">⚠️ Reported problem: {selectedItem.issue}</p>
                        {/if}
                        {#if selectedItem.lab_only}
                            <p class="status-message pending">🏠 Lab only. Please don't take this out of the lab.</p>
                        {/if}
                        {#if loginToRequest}
                            <p class="status-message return-pending">🔒 Needs a lab admin's approval. <a href={loginUrl()}>Log in</a> to request it.</p>
                        {:else if needsApproval}
                            <p class="status-message return-pending">🕓 Needs a lab admin's approval. You'll get a notification.</p>
                        {/if}
                        {#if selectedItem.tracked}
                            <label class="accept">
                                <input type="checkbox" bind:checked={borrowForm.accept} required />
                                <span>I take responsibility for this {selectedItem.name} until I return it or hand it on.</span>
                            </label>
                        {/if}
                    {:else if itemKey(borrowForm.item_name)}
                        <small class="help-text">✨ New item. It'll be added to the catalogue.</small>
                    {:else}
                        <small class="help-text">Pick from the list, or type a new item's name to add it.</small>
                    {/if}
                </div>

                {#if !selectedItem && itemKey(borrowForm.item_name)}
                    <div class="form-group">
                        <label for="lab">Lab Location *</label>
                        <select id="lab" bind:value={borrowForm.lab_location} required>
                            <option value="">Select the lab where the item lives</option>
                            {#each labLocations as lab}
                                <option value={lab.value}>{lab.label}</option>
                            {/each}
                        </select>
                    </div>
                {/if}

                <div class="form-group">
                    <label for="quantity">Quantity *</label>
                    <input type="number" id="quantity" inputmode="numeric" bind:value={borrowForm.quantity_borrowed} min="1" required />
                    {#if freeCount !== null && borrowForm.quantity_borrowed > freeCount}
                        <small class="help-text warn-text">The lab should only have {freeCount} free. You'll be asked to confirm.</small>
                    {/if}
                </div>

                <div class="form-group">
                    <span class="label" id="return-label">Return Time *</span>
                    <div class="chip-row" role="group" aria-labelledby="return-label">
                        {#each durationPresets as preset}
                            <button
                                type="button"
                                class="chip"
                                class:selected={!showCustomDuration && borrowForm.due_date === dateIn(preset.days)}
                                on:click={() => { borrowForm.due_date = dateIn(preset.days); showCustomDuration = false; }}
                            >
                                {preset.label}
                            </button>
                        {/each}
                        <button type="button" class="chip" class:selected={showCustomDuration} on:click={() => (showCustomDuration = !showCustomDuration)}>
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
                    <small class="help-text">
                        Expected return: by the end of <strong class="mono">{shortDay(borrowForm.due_date)}</strong> ({daysAway(borrowForm.due_date)})
                    </small>
                </div>

                <div class="form-group">
                    <label for="purpose">Purpose <span class="optional-tag">optional</span></label>
                    <div class="chip-row">
                        {#each purposePresets as preset}
                            <button type="button" class="chip" class:selected={borrowForm.purpose === preset} on:click={() => pickPurpose(preset)}>
                                {preset}
                            </button>
                        {/each}
                    </div>
                    <textarea id="purpose" bind:value={borrowForm.purpose} placeholder="Anything more specific? (project name, priority)" rows="2"></textarea>
                </div>

                <div class="form-group">
                    <label for="photo">Item Photo <span class="optional-tag">optional</span></label>
                    <input
                        type="file"
                        id="photo"
                        accept="image/jpeg,image/jpg,image/png,image/webp,image/gif,image/bmp"
                        capture="environment"
                        bind:this={photoInput}
                        on:change={handlePhotoUpload}
                    />
                    <small class="help-text">Take a photo or upload an image (JPG, PNG, WEBP, GIF, BMP - max 10MB). It shows the condition the item left in.</small>
                    {#if borrowForm.item_photo}
                        <div class="photo-preview">
                            {#if photoPreview}
                                <img src={photoPreview} alt="Selected item" />
                            {/if}
                            <p>✅ Photo selected: {borrowForm.item_photo.name} ({(borrowForm.item_photo.size / 1024 / 1024).toFixed(2)}MB)</p>
                            <button type="button" class="remove-photo" on:click={clearPhoto}>Remove</button>
                        </div>
                    {/if}
                </div>

                <button type="submit" class="submit-btn" disabled={isSubmitting}>
                    {isSubmitting ? 'Processing...' : loginToRequest ? 'Log in to Request' : needsApproval ? 'Request to Borrow' : 'Borrow Item'}
                </button>
            </form>
        </div>
    {/if}

    <!-- Return View -->
    {#if currentView === 'return'}
        <div class="return-container">
            <h2 class="view-title return-title">↩️ Return Equipment</h2>
            <p class="view-sub">Your own loans are at the top.{#if !user}{' '}Borrowed without an account? Return it with the phone number you used.{/if}</p>

            <div class="search-container">
                <label for="search">Search by name or item:</label>
                <div class="search-input-group">
                    <input
                        type="text"
                        id="search"
                        bind:value={searchQuery}
                        placeholder="Enter name or item name..."
                        autocomplete="off"
                    />
                    {#if searchQuery && searchQuery.trim()}
                        <button class="clear-search-btn" on:click={clearSearch} title="Clear search" aria-label="Clear search" type="button">
                            <span aria-hidden="true">✕</span>
                        </button>
                    {/if}
                </div>
            </div>

            {#if loading && !filteredLoans.length}
                <div class="loading-state">
                    <p>🔄 Loading items...</p>
                </div>
            {:else if filteredLoans.length === 0}
                <div class="no-items-state">
                    <p>📭 No items found.</p>
                    {#if searchQuery.trim()}
                        <p class="search-suggestion">Try different search terms or clear the search to see all items.</p>
                    {:else}
                        <p class="search-suggestion">Nothing is out right now.</p>
                    {/if}
                </div>
            {:else}
                <div class="items-list">
                    {#if searchQuery.trim()}
                        <h3 class="results-header">Search Results ({filteredLoans.length} found)</h3>
                    {:else}
                        <h3 class="results-header">All Items ({filteredLoans.length} total)</h3>
                    {/if}

                    {#each filteredLoans as loan (loan.ID)}
                        {@const itemCategory = loan.status === 'not_found' ? 'missing' : loan.status === 'returned' ? 'returned' : 'borrowed'}
                        {@const late = itemCategory === 'borrowed' && !!loan.expected_return_date && isOverdue(loan.expected_return_date)}
                        {@const mine = isMine(loan)}

                        <div class="item-card {itemCategory}" class:overdue={late} class:own={mine}>
                            <div class="category-tag {itemCategory}" class:overdue={late}>
                                {#if itemCategory === 'missing'}
                                    <span class="tag-icon">⚠️</span>
                                    <span class="tag-text">Missing Item</span>
                                {:else if itemCategory === 'returned'}
                                    <span class="tag-icon">✅</span>
                                    <span class="tag-text">Returned Item</span>
                                {:else if late}
                                    <span class="tag-icon">⏰</span>
                                    <span class="tag-text">Overdue Item</span>
                                {:else}
                                    <span class="tag-icon">📋</span>
                                    <span class="tag-text">Borrowed Item</span>
                                {/if}
                                {#if mine}
                                    <span class="mine-badge">Yours</span>
                                {:else if isProbablyMine(loan) && itemCategory === 'borrowed'}
                                    <span class="mine-badge">Yours?</span>
                                {/if}
                            </div>

                            <div class="item-content">
                                <div class="item-header">
                                    <h4 class="item-name">{loan.item_name}</h4>
                                </div>

                                <div class="item-details">
                                    <div class="detail-row">
                                        <div class="detail-item">
                                            <strong>Borrower:</strong> {loan.borrower_name}
                                        </div>
                                        <div class="detail-item">
                                            <strong>Lab:</strong> {loan.lab_location}
                                        </div>
                                    </div>

                                    <div class="detail-row">
                                        <div class="detail-item">
                                            <strong>Quantity:</strong> <span class="mono">{loan.quantity_borrowed}</span>
                                        </div>
                                    </div>

                                    <div class="detail-row">
                                        <div class="detail-item">
                                            <strong>Borrowed:</strong> <span class="mono">{formatDate(loan.CreatedAt)}</span>
                                        </div>
                                        {#if loan.expected_return_date}
                                            <div class="detail-item" class:late-text={late}>
                                                <strong>Expected Return:</strong> {formatExpectedReturn(loan.expected_return_date)}
                                            </div>
                                        {/if}
                                    </div>

                                    {#if loan.photo_filename}
                                        <div class="item-photo">
                                            <a href="/api/photos/{loan.photo_filename}" target="_blank" rel="noopener noreferrer">
                                                <img src="/api/photos/{loan.photo_filename}" alt={loan.item_name} class="photo-thumbnail" loading="lazy" />
                                            </a>
                                        </div>
                                    {/if}
                                </div>

                                <div class="item-actions">
                                    {#if itemCategory === 'missing'}
                                        <p class="status-message missing">⚠️ Marked missing by an admin. If you find it, tell lab staff.</p>
                                    {:else if itemCategory === 'returned'}
                                        <p class="status-message returned">✅ Successfully returned.</p>
                                    {:else if mine}
                                        <button class="return-action-btn" on:click={() => returnMine(loan)} disabled={loading}>
                                            ✅ Mark as Returned
                                        </button>
                                    {:else if loan.user_id}
                                        <button class="return-action-btn handover-btn" on:click={() => takeOver(loan)} disabled={loading}>
                                            🤝 I have this now
                                        </button>
                                        <small class="help-text">Got it straight from {loan.borrower_name}? Move it to your account.</small>
                                    {:else if returningLoanId === loan.ID}
                                        <form class="return-confirm" on:submit|preventDefault={() => returnItem(loan.ID)}>
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
                                                <p class="return-error" role="alert">{returnError}</p>
                                            {/if}
                                            <div class="return-confirm-actions">
                                                <button type="submit" class="return-action-btn" disabled={loading}>
                                                    ✅ Confirm Return
                                                </button>
                                                <button type="button" class="return-cancel-btn" on:click={cancelReturn}>
                                                    Cancel
                                                </button>
                                            </div>
                                        </form>
                                    {:else}
                                        <button class="return-action-btn" on:click={() => startReturn(loan.ID)} disabled={loading}>
                                            ✅ Mark as Returned
                                        </button>
                                    {/if}
                                </div>
                            </div>
                        </div>
                    {/each}
                </div>
            {/if}
        </div>
    {/if}

    <!-- Printer Guidelines View -->
    {#if currentView === 'printer-guide'}
        <div class="printer-guide-container">
            <h2 class="view-title guide-title">🖨️ Printer guide</h2>
            <p class="view-sub">How to print on the lab's Bambu Lab P1S printers, step by step.</p>

            <div class="guide-content">
                <!-- The one thing everyone must remember, before anything else -->
                <div class="rule-box">
                    <strong>The one rule: put your name in the file name.</strong>
                    <p>
                        Name every print <code>yourname_object</code>, for example
                        <code>srinath_bracket.3mf</code>. The <a href="/printers">printer page</a> shows
                        the file name, so everyone can see whose print is running. A print with no
                        name on it may be stopped when someone else needs the printer.
                    </p>
                </div>

                <section class="guide-section">
                    <h3>🚀 1. First-time setup</h3>
                    <p class="section-intro">Do this once on your laptop. Next time, skip straight to step 2.</p>
                    <ol class="steps">
                        <li>
                            <strong>Install a slicer.</strong>
                            It turns your 3D model into instructions the printer understands
                            ("slicing"). Either one works with our printers:
                            <ul>
                                <li>
                                    <strong>Bambu Studio</strong> (recommended - the RRC presets below are made for it):
                                    <a href="https://bambulab.com/en/download/studio" target="_blank" rel="noopener noreferrer">download from bambulab.com</a>
                                </li>
                                <li>
                                    <strong>OrcaSlicer</strong>:
                                    <a href="https://www.orcaslicer.com/download/" target="_blank" rel="noopener noreferrer">download from orcaslicer.com</a>
                                </li>
                            </ul>
                        </li>
                        <li>
                            <strong>Download the RRC presets</strong> and import them.
                            They are the lab's tested settings for our printers and our filament.
                            <a class="guide-link download-link" href="/downloads/RRC_Bambu_P1S_Preset.bbscfg" download>
                                <span class="link-icon">📥</span>
                                <span class="link-content">
                                    <strong>RRC_Bambu_P1S_Preset.bbscfg</strong>
                                    <span class="link-desc">Printer, print and filament presets for the lab's P1S printers</span>
                                </span>
                            </a>
                            In Bambu Studio choose <em>File → Import → Import Configs…</em>, pick the
                            file, and restart Bambu Studio if the presets do not show up.
                        </li>
                        <li>
                            <strong>Join the printer network.</strong>
                            The printers have their own network inside the lab. You cannot reach
                            them from wifi@iiith.
                            <div class="wifi-box">
                                <div class="wifi-row">
                                    <span class="wifi-label">Network</span>
                                    <code>printers@rrc</code>
                                </div>
                                <div class="wifi-row">
                                    <span class="wifi-label">Password</span>
                                    {#if showWifiPassword}
                                        <code>printers@1234</code>
                                        <button class="reveal-btn" on:click={() => (showWifiPassword = false)}>Hide</button>
                                    {:else}
                                        <code class="hidden-password">••••••••••••</code>
                                        <button class="reveal-btn" on:click={() => (showWifiPassword = true)}>Show</button>
                                    {/if}
                                </div>
                                <p class="wifi-note">Lab members only. Do not post it in group chats or share it outside the lab.</p>
                            </div>
                        </li>
                        <li>
                            <strong>Add the printers in Bambu Studio</strong> (only if you want to
                            send prints from Bambu Studio itself). Use <em>LAN mode</em> and enter the
                            printer's IP address and access code. The access code is on the printer's
                            screen under <em>Settings → Network</em>; it is not listed here on purpose.
                            <div class="printer-table-wrap">
                                <table class="printer-table">
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
                        </li>
                    </ol>
                </section>

                <section class="guide-section">
                    <h3>🎛️ 2. Pick the right presets</h3>
                    <p class="section-intro">
                        In Bambu Studio, choose these three things before you slice. Getting them
                        right is most of what makes a print work.
                    </p>

                    <h4 class="sub-head">Printer</h4>
                    <p class="body-text">Always <code>Bambu Lab P1S 0.4 nozzle</code>.</p>

                    <h4 class="sub-head">Print settings (process)</h4>
                    <div class="printer-table-wrap">
                        <table class="printer-table wide">
                            <thead>
                                <tr><th>Preset</th><th>Use it for</th><th>What it does</th></tr>
                            </thead>
                            <tbody>
                                <tr>
                                    <td><strong>RRC Printing Default</strong></td>
                                    <td>Almost every print. Start here.</td>
                                    <td>0.2 mm layers, 20% gyroid infill, tree supports on, brim on the outside.</td>
                                </tr>
                                <tr>
                                    <td><strong>Ironing</strong></td>
                                    <td>Parts with flat tops that should look smooth.</td>
                                    <td>Same as the default, plus a final pass that smooths the top surface. Takes longer.</td>
                                </tr>
                            </tbody>
                        </table>
                    </div>
                    <span class="step-note">
                        Supports are switched on in both. If your model has no overhangs, turn
                        supports off to save time and filament. The full preset names end in
                        <em>@BBL X1C</em>; that is normal, the P1S uses the same print settings.
                    </span>

                    <h4 class="sub-head">Filament</h4>
                    <p class="body-text">
                        Pick the preset for the filament that is <strong>actually loaded</strong> in
                        the printer you will use. Check on the printer page or the printer's screen.
                    </p>
                    <div class="printer-table-wrap">
                        <table class="printer-table wide">
                            <thead>
                                <tr><th>Loaded filament</th><th>Preset</th><th>Nozzle</th><th>Bed</th></tr>
                            </thead>
                            <tbody>
                                <tr>
                                    <td>Numakers PLA+</td>
                                    <td><code>Numakers PLA +</code></td>
                                    <td class="nowrap">220 °C</td>
                                    <td>55 °C smooth plate, 65 °C textured plate</td>
                                </tr>
                                <tr>
                                    <td>Numakers PLA Silk</td>
                                    <td><code>Numakers PLA Silk</code></td>
                                    <td class="nowrap">230 °C</td>
                                    <td class="nowrap">55 °C</td>
                                </tr>
                                <tr>
                                    <td>Numakers ABS</td>
                                    <td><code>Numakers ABS ABS</code></td>
                                    <td class="nowrap">260 °C</td>
                                    <td class="nowrap">90 °C</td>
                                </tr>
                                <tr>
                                    <td>Numakers ABS, tall or thin parts</td>
                                    <td><code>Numakers ABS ABS … 0.8 Retract</code></td>
                                    <td class="nowrap">260 °C</td>
                                    <td class="nowrap">90 °C</td>
                                </tr>
                            </tbody>
                        </table>
                    </div>
                    <span class="step-note">
                        The temperatures are already in the presets; they are listed so you can
                        check them. The "0.8 Retract" version lifts the nozzle 0.8 mm when it moves,
                        so it does not knock over or scrape tall, thin ABS parts.
                    </span>
                    <span class="danger-note">
                        Never print one material with another material's preset. The wrong
                        temperature can <strong>clog the nozzle</strong>, and a clog takes the printer
                        out of service until it is repaired.
                    </span>

                    <h4 class="sub-head">Printing ABS</h4>
                    <ul class="plain-list">
                        <li>Keep the printer's lid and door <strong>closed</strong> for the whole print. A draught makes ABS warp and lift off the bed.</li>
                        <li>ABS gives off fumes. Keep the room ventilated and do not lean over the printer while it prints.</li>
                        <li>Use a textured or engineering plate, not the smooth plate.</li>
                    </ul>
                </section>

                <section class="guide-section">
                    <h3>🔪 3. Slice and check</h3>
                    <ol class="steps">
                        <li>
                            <strong>Set the plate type</strong> in Bambu Studio to the plate that is
                            on the printer (smooth or textured). The bed temperature depends on it.
                        </li>
                        <li>
                            <strong>Slice</strong>, then look at the preview. Check the estimated
                            time and how much filament it needs, and make sure there is enough left
                            on the spool.
                        </li>
                        <li>
                            <strong>First few prints?</strong> Show your settings to a lab admin before
                            you start. A 30-second check can save a failed 10-hour print.
                        </li>
                        <li>
                            <strong>Export</strong> the sliced file and name it
                            <code>yourname_object</code>.
                        </li>
                    </ol>
                </section>

                <section class="guide-section">
                    <h3>📤 4. Send it to the printer</h3>
                    <ol class="steps">
                        <li>
                            <strong>Check the printer is free.</strong> Open the
                            <a href="/printers">printer page</a>. Look at the <em>camera</em>, not only
                            the status: a printer can say "Finished" while the last person's print is
                            still on the plate.
                        </li>
                        <li>
                            <strong>Send the file.</strong> Either:
                            <ul class="sub-steps">
                                <li>
                                    <strong>From this website</strong> (easiest, no wifi switching):
                                    log in, open <a href="/printers">3D printers</a>, choose the
                                    printer, click <em>Send a file to print</em> and drop your file in.
                                </li>
                                <li>
                                    <strong>From Bambu Studio</strong>, if you are on the printer
                                    network and have added the printer (step 1).
                                </li>
                            </ul>
                        </li>
                        <li>
                            <strong>Go to the printer and check it</strong> before you start: the
                            plate is empty and clean, and the right filament is loaded with enough on
                            the spool. Then start the print on the printer's screen.
                        </li>
                    </ol>
                </section>

                <section class="guide-section">
                    <h3>👀 5. During and after the print</h3>
                    <ol class="steps">
                        <li>
                            <strong>Watch the first layer.</strong> Stay until it is down: most
                            failures happen there.
                        </li>
                        <li>
                            <strong>Check in regularly</strong> on the <a href="/printers">printer
                            page</a>. The camera shows whether it is still printing properly. Do not
                            start a print and forget about it.
                        </li>
                        <li>
                            <strong>Collect your print soon after it finishes</strong> and clear the
                            plate, so the next person can start. Take your waste (purge blobs,
                            supports, brim) with you.
                        </li>
                    </ol>
                </section>

                <section class="guide-section warning-section">
                    <h3>⚠️ When to stop a print</h3>
                    <p class="section-intro">
                        Stop the print on the printer's screen <strong>straight away</strong> and tell
                        a lab admin if you see any of these. Anyone may stop a print that is going
                        wrong. It is always better to stop early than to damage the printer.
                    </p>
                    <div class="warning-list">
                        <div class="warning-item">
                            <span class="warning-icon" aria-hidden="true">🚫</span>
                            <div class="warning-content">
                                <strong>No plastic coming out of the nozzle</strong>
                                <p>Stop. The nozzle may be clogged. Do not try to clear it yourself.</p>
                            </div>
                        </div>
                        <div class="warning-item">
                            <span class="warning-icon" aria-hidden="true">🌀</span>
                            <div class="warning-content">
                                <strong>Tangled filament, or printing in mid-air ("spaghetti")</strong>
                                <p>Stop. Do not touch the hot nozzle or bed.</p>
                            </div>
                        </div>
                        <div class="warning-item">
                            <span class="warning-icon" aria-hidden="true">❌</span>
                            <div class="warning-content">
                                <strong>Print came loose, or looks badly wrong</strong>
                                <p>Stop to save filament. Take a photo if you can.</p>
                            </div>
                        </div>
                        <div class="warning-item">
                            <span class="warning-icon" aria-hidden="true">📟</span>
                            <div class="warning-content">
                                <strong>An error on the printer's screen</strong>
                                <p>Take a photo of the error <em>before</em> clearing it, then tell an admin.</p>
                            </div>
                        </div>
                        <div class="warning-item community-item">
                            <span class="warning-icon" aria-hidden="true">👥</span>
                            <div class="warning-content">
                                <p>If you stop someone else's print, tell them or a lab admin.</p>
                            </div>
                        </div>
                    </div>
                </section>

                <section class="guide-section">
                    <h3>🧵 Loading and unloading filament</h3>
                    <p class="section-intro">
                        Always use the menu on the printer's screen. Never pull filament out cold or
                        force it: that is how nozzles get damaged.
                    </p>
                    <div class="two-col">
                        <div class="col-box">
                            <h4>Loading</h4>
                            <ol class="mini-steps">
                                <li>Put the spool on the holder so it unwinds without tangling.</li>
                                <li>Cut the end at an angle so it feeds in cleanly.</li>
                                <li>Push it through the tube until it reaches the extruder.</li>
                                <li>On the screen, choose <em>Filament → Load</em> and pick the material.</li>
                                <li>Wait until only the new colour comes out of the nozzle.</li>
                            </ol>
                        </div>
                        <div class="col-box">
                            <h4>Unloading</h4>
                            <ol class="mini-steps">
                                <li>On the screen, choose <em>Filament → Unload</em>.</li>
                                <li>Wait for the nozzle to heat up. Do not skip this.</li>
                                <li>Let the printer pull it back, then pull gently. If it resists, stop and ask an admin.</li>
                                <li>Clip the end into the spool so it does not unwind for the next person.</li>
                            </ol>
                        </div>
                    </div>
                </section>

                <section class="guide-section">
                    <h3>🦺 Safety</h3>
                    <ul class="plain-list">
                        <li>
                            <strong>Be careful with the scraper.</strong> The blade is sharp and
                            prints come loose suddenly. Take the plate off the printer first and
                            scrape <em>away</em> from your hand. The scraper hurts more people than
                            anything else on these printers.
                        </li>
                        <li>
                            <strong>The nozzle and bed are hot</strong>: the nozzle is over 200 °C.
                            Let them cool before you reach inside.
                        </li>
                        <li>
                            <strong>Keep the lid closed while printing.</strong> Never reach in to fix
                            something mid-print. Pause or stop it first.
                        </li>
                    </ul>
                </section>

                <section class="guide-section">
                    <h3>📋 Lab rules</h3>
                    <ul class="plain-list">
                        <li>Put your name in the file name. Every time.</li>
                        <li>Use the RRC presets. Do not change printer settings, calibration or firmware.</li>
                        <li>Ask before using filament that is not yours. If you are not sure whose it is, ask; do not assume it is lab stock.</li>
                        <li>Do not cancel or remove someone else's print unless it is failing (see above). Otherwise ask an admin.</li>
                        <li>Do not start a long print and disappear without telling anyone.</li>
                        <li>Keep the printer area clean: clear purge blobs and supports, put tools back.</li>
                        <li>If you break something, say so. Hiding it costs the lab more.</li>
                    </ul>
                </section>

                <section class="guide-section">
                    <h3>🤔 Common questions</h3>
                    <ul class="plain-list">
                        <li>
                            <strong>A printer shows Offline?</strong> It is switched off or off the
                            network. Tell an admin.
                        </li>
                        <li>
                            <strong>Your print stopped and you do not know why?</strong> An admin may
                            have stopped it; the printer page shows who. Ask them.
                        </li>
                        <li>
                            <strong>A printer says "Access code changed"?</strong> Nothing you did.
                            Tell an admin so they can update it.
                        </li>
                        <li>
                            <strong>Your print is failing and you are not in the lab?</strong> Message
                            a lab admin. Admins can stop any print from the printer page.
                        </li>
                    </ul>
                </section>

                <section class="guide-section">
                    <h3>📚 Official Bambu Lab guides</h3>
                    <div class="link-grid">
                        <a class="guide-link" href="https://wiki.bambulab.com/en/p1/manual/loading-filament" target="_blank" rel="noopener noreferrer">
                            <span class="link-icon">📥</span>
                            <span class="link-content">
                                <strong>Loading filament</strong>
                                <span class="link-desc">Step-by-step, with pictures</span>
                            </span>
                        </a>
                        <a class="guide-link" href="https://wiki.bambulab.com/en/p1/manual/print-from-bambu-studio" target="_blank" rel="noopener noreferrer">
                            <span class="link-icon">🚀</span>
                            <span class="link-content">
                                <strong>Printing from Bambu Studio</strong>
                                <span class="link-desc">Sending a print over the network</span>
                            </span>
                        </a>
                        <a class="guide-link" href="https://wiki.bambulab.com/en/p1/manual" target="_blank" rel="noopener noreferrer">
                            <span class="link-icon">📘</span>
                            <span class="link-content">
                                <strong>P1S manual</strong>
                                <span class="link-desc">Everything else about the printer</span>
                            </span>
                        </a>
                        <a class="guide-link" href="https://wiki.bambulab.com/en/general/filament-guide-material-table" target="_blank" rel="noopener noreferrer">
                            <span class="link-icon">📖</span>
                            <span class="link-content">
                                <strong>Filament guide</strong>
                                <span class="link-desc">Which material to use for what</span>
                            </span>
                        </a>
                    </div>
                </section>

                <section class="guide-section contact-section">
                    <h3>📞 Need help?</h3>
                    <p>Ask a lab admin. It is always better to stop and ask than to damage a printer.</p>
                </section>
            </div>
        </div>
    {/if}
</div>

<style>
    /* Home */
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

    /* Home: the menu of tiles, one row each - icon, text, arrow */
    .mine {
        max-width: 1000px;
        margin: 0 auto 18px;
    }

    .home-options {
        text-align: center;
    }

    .home-options h2 {
        margin: 8px 0 0;
        font-size: clamp(1.25rem, 3vw, 1.6rem);
        font-weight: 500;
        color: var(--rp-text);
    }

    .option-buttons {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
        gap: clamp(12px, 2vw, 18px);
        max-width: 1000px;
        margin: clamp(18px, 3vw, 28px) auto 0;
    }

    .option-btn {
        --tile-accent: var(--rp-iris);
        position: relative;
        display: flex;
        align-items: center;
        gap: 14px;
        width: 100%;
        min-height: 84px;
        box-sizing: border-box;
        text-align: left;
        text-decoration: none;
        padding: clamp(14px, 2.4vw, 20px);
        border: 1px solid var(--line-strong);
        border-radius: var(--radius-lg);
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--rp-text) 4%, transparent), transparent 60%),
            var(--card);
        color: var(--rp-text);
        font: inherit;
        cursor: pointer;
        overflow: hidden;
        box-shadow: var(--shadow-sm);
        transition:
            transform var(--normal) var(--ease),
            border-color var(--normal) var(--ease),
            box-shadow var(--normal) var(--ease);
        animation: rise var(--slow) var(--ease) both;
        animation-delay: calc(var(--i, 0) * 50ms);
    }

    /* A colour wash that grows from the left edge on hover */
    .option-btn::before {
        content: '';
        position: absolute;
        inset: 0;
        background: linear-gradient(90deg, color-mix(in srgb, var(--tile-accent) 18%, transparent), transparent 55%);
        opacity: 0;
        transition: opacity var(--normal) var(--ease);
        pointer-events: none;
    }

    /* The accent stripe down the leading edge */
    .option-btn::after {
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

    .option-btn:hover,
    .option-btn:focus-visible {
        transform: translateY(-3px);
        border-color: color-mix(in srgb, var(--tile-accent) 55%, var(--line-strong));
        box-shadow: var(--shadow);
    }

    .option-btn:hover::before,
    .option-btn:focus-visible::before {
        opacity: 1;
    }

    .option-btn:hover::after,
    .option-btn:focus-visible::after {
        transform: scaleY(1);
    }

    .option-btn:active {
        transform: translateY(-1px) scale(0.995);
    }

    .tile-icon {
        font-size: 1.85rem;
        line-height: 1;
        flex-shrink: 0;
        transition: transform var(--normal) var(--ease);
    }

    .tile-image {
        width: 46px;
        height: 46px;
        object-fit: contain;
        flex-shrink: 0;
        transition: transform var(--normal) var(--ease);
    }

    .option-btn:hover .tile-icon,
    .option-btn:hover .tile-image {
        transform: scale(1.08) rotate(-2deg);
    }

    .tile-body {
        display: flex;
        flex-direction: column;
        gap: 3px;
        min-width: 0;
        flex: 1;
    }

    .tile-title {
        font-size: clamp(1rem, 2vw, 1.12rem);
        font-weight: 650;
        color: var(--tile-accent);
    }

    .tile-sub {
        font-size: clamp(0.82rem, 1.6vw, 0.9rem);
        color: var(--muted);
        line-height: 1.35;
    }

    .tile-arrow {
        flex-shrink: 0;
        color: var(--faint);
        font-size: 1.1rem;
        opacity: 0;
        transform: translateX(-4px);
        transition: transform var(--normal) var(--ease), opacity var(--normal) var(--ease), color var(--normal) var(--ease);
    }

    .option-btn:hover .tile-arrow,
    .option-btn:focus-visible .tile-arrow {
        opacity: 1;
        transform: none;
        color: var(--tile-accent);
    }

    /* Phones: there is no hover, so show the arrow, and use one column */
    @media (max-width: 640px) {
        .option-buttons {
            grid-template-columns: 1fr;
        }

        .tile-arrow {
            opacity: 1;
            transform: none;
        }
    }

    /* --- Borrow / Return / Guide: the original lab style ------------------ */

    .view-title {
        margin: 0;
        font-size: clamp(1.35rem, 3vw, 1.7rem);
        font-weight: 650;
    }

    .borrow-title { color: var(--rp-rose); }
    .return-title { color: var(--rp-foam); }
    .guide-title { color: var(--rp-gold); }

    .view-sub {
        margin: 6px 0 22px;
        color: var(--muted);
        font-size: clamp(0.9rem, 2vw, 1rem);
    }

    .label-row {
        display: flex;
        justify-content: space-between;
        align-items: baseline;
        gap: 8px;
    }

    .link-btn {
        background: none;
        border: none;
        color: var(--link);
        font: inherit;
        font-size: 0.85rem;
        cursor: pointer;
        padding: 10px 0;
        margin: -10px 0;
        text-decoration: underline;
    }

    .optional-tag {
        font-size: 0.75rem;
        color: var(--faint);
        font-weight: 400;
    }

    .chip-row {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-bottom: 8px;
    }

    .chip {
        display: inline-flex;
        align-items: center;
        background: var(--card-raised);
        color: var(--rp-text);
        border: 1px solid var(--line-strong);
        border-radius: 999px;
        padding: 10px 16px;
        /* Comfortable one-handed tap target on a phone */
        min-height: 44px;
        font: inherit;
        font-size: 0.95rem;
        cursor: pointer;
        transition: background var(--fast) var(--ease), border-color var(--fast) var(--ease);
    }

    .chip:hover {
        background: var(--rp-hl-med);
    }

    .chip.selected {
        background: var(--rp-rose);
        border-color: var(--rp-rose);
        color: var(--rp-base);
        font-weight: 600;
    }

    /* Form Styles */
    .form-container,
    .return-container,
    .printer-guide-container {
        background: var(--card);
        padding: clamp(16px, 3vw, 25px);
        border-radius: 12px;
        margin: 0 auto;
        border: 1px solid var(--line-strong);
        box-shadow: var(--shadow);
        max-width: 800px;
        box-sizing: border-box;
        animation: rise var(--slow) var(--ease) both;
    }

    .printer-guide-container {
        max-width: 1000px;
    }

    .form-group {
        margin-bottom: clamp(18px, 3vw, 24px);
        min-width: 0;
    }

    .form-group label,
    .form-group .label {
        display: block;
        margin: 0 0 8px;
        font-weight: 600;
        color: var(--rp-text);
        font-size: clamp(0.92rem, 2vw, 1rem);
    }

    .form-group input:not([type='checkbox']),
    .form-group select,
    .form-group textarea {
        width: 100%;
        min-height: 46px;
        padding: clamp(10px, 2.5vw, 13px);
        border: 2px solid var(--line-strong);
        border-radius: 8px;
        font: inherit;
        font-size: clamp(0.95rem, 2vw, 1rem);
        box-sizing: border-box;
        background: var(--bg);
        color: var(--rp-text);
        transition: border-color var(--fast) var(--ease), box-shadow var(--fast) var(--ease);
    }

    .form-group textarea {
        resize: vertical;
        min-height: 70px;
    }

    .form-group input[type='file'] {
        padding: 10px;
        cursor: pointer;
    }

    .form-group input:focus,
    .form-group select:focus,
    .form-group textarea:focus {
        border-color: var(--rp-rose);
        outline: none;
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--rp-rose) 25%, transparent);
    }

    .help-text {
        color: var(--muted);
        font-size: clamp(0.82rem, 1.8vw, 0.9rem);
        margin-top: 6px;
        display: block;
        line-height: 1.45;
    }

    .help-text strong {
        color: var(--rp-text);
    }

    .warn-text {
        color: var(--warn);
    }

    /* Who is borrowing */
    .login-hint {
        margin-bottom: 20px;
        padding: 12px 14px;
        border-radius: 8px;
        border: 1px dashed color-mix(in srgb, var(--rp-iris) 50%, transparent);
        background: color-mix(in srgb, var(--rp-iris) 8%, transparent);
        color: var(--rp-text);
        font-size: 0.92rem;
    }

    .login-hint a,
    .status-message a {
        color: var(--link);
        font-weight: 600;
    }

    .account-box {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        justify-content: space-between;
        gap: 6px 14px;
        padding: 12px 14px;
        border: 2px solid var(--line-strong);
        border-radius: 8px;
        background: var(--bg);
    }

    .account-name {
        font-weight: 600;
    }

    .account-phone {
        color: var(--muted);
    }

    /* What we know about the picked item */
    .item-info {
        margin-top: 10px;
    }

    .info-row {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
    }

    .info-chip {
        padding: 4px 10px;
        border-radius: 999px;
        font-size: 0.82rem;
        background: color-mix(in srgb, var(--rp-foam) 12%, transparent);
        color: var(--rp-foam);
        border: 1px solid color-mix(in srgb, var(--rp-foam) 30%, transparent);
    }

    .info-chip.short {
        background: color-mix(in srgb, var(--rp-gold) 12%, transparent);
        color: var(--warn);
        border-color: color-mix(in srgb, var(--rp-gold) 35%, transparent);
    }

    .form-group .status-message {
        margin-top: 10px;
        font-size: 0.92rem;
        font-weight: 500;
        text-align: left;
    }

    .form-group .accept {
        display: flex;
        gap: 10px;
        align-items: flex-start;
        margin: 12px 0 0;
        padding: 12px;
        border-radius: 8px;
        background: color-mix(in srgb, var(--rp-gold) 10%, transparent);
        font-weight: 500;
        font-size: 0.92rem;
        cursor: pointer;
    }

    .accept input {
        width: 20px;
        height: 20px;
        margin: 1px 0 0;
        flex: none;
        accent-color: var(--rp-rose);
    }

    .photo-preview {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 10px 12px;
        margin-top: 10px;
        padding: 12px;
        background: color-mix(in srgb, var(--ctp-green) 15%, transparent);
        border-radius: 8px;
        border: 1px solid color-mix(in srgb, var(--ctp-green) 30%, transparent);
    }

    .photo-preview img {
        width: 64px;
        height: 64px;
        object-fit: cover;
        border-radius: 6px;
        flex: none;
    }

    .photo-preview p {
        flex: 1;
        min-width: 0;
        margin: 0;
        color: var(--ctp-green);
        font-size: clamp(0.82rem, 1.8vw, 0.9rem);
        overflow-wrap: anywhere;
    }

    .remove-photo {
        min-height: 40px;
        padding: 0 14px;
        border-radius: 8px;
        border: 1px solid var(--line-strong);
        background: var(--card-raised);
        color: var(--rp-text);
        font: inherit;
        font-size: 0.85rem;
        cursor: pointer;
    }

    .submit-btn {
        background: linear-gradient(135deg, var(--rp-rose), var(--rp-love));
        color: var(--rp-base);
        border: none;
        min-height: 50px;
        padding: clamp(12px, 3vw, 16px) clamp(20px, 4vw, 30px);
        border-radius: 8px;
        font: inherit;
        font-size: clamp(1rem, 2.5vw, 1.15rem);
        cursor: pointer;
        width: 100%;
        transition: transform var(--normal) var(--ease), box-shadow var(--normal) var(--ease);
        font-weight: 650;
        margin-top: 6px;
    }

    .submit-btn:hover {
        transform: translateY(-2px);
        box-shadow: 0 8px 25px color-mix(in srgb, var(--rp-rose) 30%, transparent);
    }

    .submit-btn:disabled {
        background: var(--card-raised);
        cursor: not-allowed;
        transform: none;
        box-shadow: none;
        color: var(--muted);
    }

    /* Return Styles */
    .search-container {
        margin-bottom: clamp(18px, 4vw, 26px);
    }

    .search-container label {
        font-weight: 600;
    }

    .search-input-group {
        position: relative;
        margin-top: 8px;
    }

    .search-container input {
        width: 100%;
        min-height: 48px;
        padding: clamp(12px, 3vw, 14px);
        padding-right: 52px; /* room for the clear button */
        border: 2px solid var(--line-strong);
        border-radius: 8px;
        font: inherit;
        font-size: clamp(0.95rem, 2vw, 1rem);
        background: var(--bg);
        color: var(--rp-text);
        box-sizing: border-box;
    }

    .search-container input:focus {
        border-color: var(--rp-rose);
        outline: none;
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--rp-rose) 25%, transparent);
    }

    /* A 40px tap target around the small round ✕ */
    .clear-search-btn {
        position: absolute;
        right: 4px;
        top: 50%;
        transform: translateY(-50%);
        width: 44px;
        height: 44px;
        display: grid;
        place-items: center;
        padding: 0;
        background: none;
        border: none;
        cursor: pointer;
    }

    .clear-search-btn span {
        display: grid;
        place-items: center;
        width: 26px;
        height: 26px;
        border-radius: 50%;
        background: var(--rp-love);
        color: var(--rp-base);
        font-size: 13px;
        font-weight: bold;
        transition: transform var(--fast) var(--ease);
    }

    .clear-search-btn:hover span {
        transform: scale(1.1);
    }

    .loading-state,
    .no-items-state {
        text-align: center;
        padding: 40px 20px;
        background: var(--bg);
        border-radius: 12px;
        border: 1px solid var(--line-strong);
    }

    .loading-state p,
    .no-items-state p {
        font-size: 1.15rem;
        margin: 0;
    }

    .no-items-state .search-suggestion {
        color: var(--muted);
        font-size: 1rem;
        margin-top: 10px;
    }

    .results-header {
        background: var(--bg);
        padding: 14px 18px;
        margin: 0 0 15px;
        border-radius: 8px;
        border: 1px solid var(--line-strong);
        font-size: 1.05rem;
        font-weight: 600;
    }

    .item-card {
        background: var(--bg);
        border: 1px solid var(--line-strong);
        border-radius: 12px;
        margin-bottom: 15px;
        overflow: hidden;
        transition: transform var(--normal) var(--ease), border-color var(--normal) var(--ease), box-shadow var(--normal) var(--ease);
        box-shadow: var(--shadow-sm);
    }

    .item-card:hover {
        border-color: var(--rp-rose);
        transform: translateY(-2px);
        box-shadow: var(--shadow);
    }

    .item-card.missing { border-left: 4px solid var(--rp-love); }
    .item-card.borrowed { border-left: 4px solid var(--rp-pine); }
    .item-card.overdue { border-left: 4px solid var(--rp-gold); }
    .item-card.returned { border-left: 4px solid var(--rp-foam); }

    .category-tag {
        padding: 8px 15px;
        font-weight: 600;
        font-size: 0.85rem;
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .category-tag.missing {
        background: color-mix(in srgb, var(--rp-love) 15%, transparent);
        color: var(--rp-love);
    }

    .category-tag.borrowed {
        background: color-mix(in srgb, var(--rp-pine) 22%, transparent);
        color: var(--link);
    }

    .category-tag.overdue {
        background: color-mix(in srgb, var(--rp-gold) 15%, transparent);
        color: var(--warn);
    }

    .category-tag.returned {
        background: color-mix(in srgb, var(--rp-foam) 15%, transparent);
        color: var(--rp-foam);
    }

    .tag-text {
        text-transform: uppercase;
        letter-spacing: 0.5px;
    }

    .mine-badge {
        margin-left: auto;
        padding: 2px 10px;
        border-radius: 999px;
        background: var(--rp-iris);
        color: var(--rp-base);
        font-size: 0.75rem;
        letter-spacing: 0.3px;
    }

    .item-content {
        padding: clamp(14px, 3vw, 20px);
    }

    .item-header {
        margin-bottom: 12px;
    }

    .item-name {
        margin: 0;
        font-size: 1.25rem;
        font-weight: 600;
        overflow-wrap: anywhere;
    }

    .item-details {
        margin-bottom: 16px;
    }

    .detail-row {
        display: flex;
        gap: 8px 20px;
        margin-bottom: 10px;
        flex-wrap: wrap;
    }

    .detail-item {
        flex: 1;
        min-width: 200px;
        color: var(--muted);
        font-size: 0.95rem;
    }

    .detail-item strong {
        color: var(--rp-text);
        font-weight: 600;
    }

    .detail-item.late-text {
        color: var(--warn);
    }

    .item-photo {
        margin-top: 14px;
        text-align: center;
    }

    .photo-thumbnail {
        max-width: 300px;
        max-height: 200px;
        width: auto;
        height: auto;
        object-fit: cover;
        border-radius: 8px;
        border: 2px solid var(--line-strong);
        transition: transform var(--fast) var(--ease), border-color var(--fast) var(--ease);
    }

    .photo-thumbnail:hover {
        transform: scale(1.03);
        border-color: var(--rp-rose);
    }

    .item-actions {
        padding-top: 15px;
        border-top: 1px solid var(--line-strong);
        text-align: center;
    }

    .status-message {
        margin: 0;
        padding: 12px;
        border-radius: 8px;
        font-weight: 600;
        font-size: 0.95rem;
    }

    .status-message.missing {
        background: color-mix(in srgb, var(--rp-love) 15%, transparent);
        color: var(--rp-love);
        border: 1px solid color-mix(in srgb, var(--rp-love) 30%, transparent);
    }

    .status-message.returned {
        background: color-mix(in srgb, var(--rp-foam) 15%, transparent);
        color: var(--rp-foam);
        border: 1px solid color-mix(in srgb, var(--rp-foam) 30%, transparent);
    }

    .status-message.pending {
        background: color-mix(in srgb, var(--rp-gold) 15%, transparent);
        color: var(--warn);
        border: 1px solid color-mix(in srgb, var(--rp-gold) 30%, transparent);
    }

    .status-message.return-pending {
        background: color-mix(in srgb, var(--rp-iris) 15%, transparent);
        color: var(--rp-iris);
        border: 1px solid color-mix(in srgb, var(--rp-iris) 30%, transparent);
    }

    .return-action-btn {
        background: linear-gradient(135deg, var(--rp-foam), color-mix(in srgb, var(--rp-foam) 70%, var(--rp-pine)));
        color: var(--rp-base);
        border: none;
        min-height: 46px;
        padding: 12px 24px;
        border-radius: 8px;
        font: inherit;
        font-size: 1rem;
        font-weight: 600;
        cursor: pointer;
        transition: transform var(--normal) var(--ease), box-shadow var(--normal) var(--ease);
        display: inline-flex;
        align-items: center;
        justify-content: center;
        gap: 8px;
    }

    .return-action-btn:hover {
        transform: translateY(-2px);
        box-shadow: 0 4px 12px color-mix(in srgb, var(--rp-foam) 30%, transparent);
    }

    .return-action-btn.handover-btn {
        background: linear-gradient(135deg, var(--rp-iris), var(--rp-rose));
    }

    .return-action-btn:disabled {
        background: var(--card-raised);
        color: var(--faint);
        cursor: not-allowed;
        transform: none;
        box-shadow: none;
    }

    .return-confirm {
        display: flex;
        flex-direction: column;
        gap: 10px;
        text-align: left;
    }

    .return-confirm label {
        font-weight: 600;
    }

    .return-confirm input {
        min-height: 46px;
        padding: 12px;
        border-radius: 8px;
        border: 2px solid var(--line-strong);
        background: var(--card);
        color: var(--rp-text);
        font: inherit;
        font-size: 1rem;
    }

    .return-confirm input:focus {
        border-color: var(--rp-rose);
        outline: none;
    }

    .return-error {
        margin: 0;
        padding: 10px 12px;
        border-radius: 8px;
        background: color-mix(in srgb, var(--rp-love) 15%, transparent);
        color: var(--rp-love);
        border: 1px solid color-mix(in srgb, var(--rp-love) 30%, transparent);
    }

    .return-confirm-actions {
        display: flex;
        flex-wrap: wrap;
        gap: 10px;
        justify-content: center;
    }

    .return-cancel-btn {
        background: transparent;
        color: var(--rp-text);
        border: 1px solid var(--line-strong);
        min-height: 46px;
        padding: 12px 24px;
        border-radius: 8px;
        font: inherit;
        font-size: 1rem;
        cursor: pointer;
    }

    .item-actions .help-text {
        margin-top: 8px;
    }

    /* Printer Guide Styles */
    .guide-content {
        margin-top: 4px;
    }

    .guide-section {
        background: var(--bg);
        padding: clamp(16px, 3.5vw, 28px);
        border-radius: 10px;
        margin-bottom: 22px;
        border: 1px solid var(--line-strong);
        min-width: 0;
    }

    .guide-section h3 {
        color: var(--rp-rose);
        margin: 0 0 16px;
        font-size: clamp(1.15rem, 2.5vw, 1.4rem);
    }

    .section-intro {
        margin: 0 0 16px;
        font-size: clamp(0.92rem, 2vw, 1rem);
        line-height: 1.6;
    }

    .body-text {
        margin: 0 0 8px;
        font-size: 0.92rem;
        line-height: 1.5;
    }

    .sub-head {
        margin: 20px 0 8px;
        color: var(--rp-foam);
        font-size: 1rem;
    }

    .guide-content code {
        background: var(--card-raised);
        border: 1px solid var(--line-strong);
        border-radius: 4px;
        padding: 1px 6px;
        font-family: var(--mono);
        font-size: 0.88em;
        overflow-wrap: anywhere;
    }

    .guide-content a:not(.guide-link) {
        color: var(--link);
    }

    .rule-box {
        background: color-mix(in srgb, var(--rp-iris) 12%, var(--card));
        border: 1px solid var(--rp-iris);
        border-left: 4px solid var(--rp-iris);
        border-radius: 8px;
        padding: 14px 16px;
        margin-bottom: 22px;
    }

    .rule-box strong {
        color: var(--rp-iris);
        font-size: 1.02rem;
    }

    .rule-box p {
        margin: 8px 0 0;
        line-height: 1.5;
        font-size: 0.92rem;
    }

    .steps {
        padding-left: 20px;
        margin: 0;
    }

    .steps > li {
        margin-bottom: 14px;
        line-height: 1.5;
        font-size: 0.92rem;
    }

    .steps > li:last-child {
        margin-bottom: 0;
    }

    .steps strong {
        color: var(--warn);
    }

    .sub-steps {
        margin: 8px 0 0;
        padding-left: 18px;
    }

    .sub-steps li {
        margin-bottom: 6px;
        font-size: 0.88rem;
    }

    .step-note {
        display: block;
        margin-top: 6px;
        font-size: 0.84rem;
        color: var(--muted);
        line-height: 1.5;
    }

    .danger-note {
        display: block;
        margin-top: 10px;
        padding: 8px 10px;
        background: color-mix(in srgb, var(--rp-love) 12%, transparent);
        border-left: 3px solid var(--rp-love);
        border-radius: 4px;
        font-size: 0.86rem;
        line-height: 1.5;
    }

    .plain-list {
        padding-left: 20px;
        margin: 0;
    }

    .plain-list li {
        margin-bottom: 10px;
        line-height: 1.5;
        font-size: 0.92rem;
    }

    .wifi-box {
        background: var(--card);
        border: 1px solid var(--line-strong);
        border-radius: 8px;
        padding: 10px 12px;
        margin: 8px 0 4px;
    }

    .wifi-row {
        display: flex;
        align-items: center;
        gap: 8px;
        flex-wrap: wrap;
        margin-bottom: 6px;
    }

    .wifi-label {
        color: var(--muted);
        font-size: 0.8rem;
        min-width: 70px;
    }

    .hidden-password {
        letter-spacing: 2px;
        color: var(--faint);
    }

    .reveal-btn {
        background: var(--card-raised);
        color: var(--rp-text);
        border: 1px solid var(--line-strong);
        border-radius: 6px;
        padding: 8px 14px;
        min-height: 40px;
        font: inherit;
        font-size: 0.82rem;
        cursor: pointer;
    }

    .reveal-btn:hover {
        background: var(--rp-hl-med);
    }

    .wifi-note {
        margin: 4px 0 0;
        font-size: 0.78rem;
        color: var(--faint);
    }

    .printer-table-wrap {
        overflow-x: auto;
        margin: 10px 0 4px;
        max-width: 100%;
    }

    .printer-table {
        border-collapse: collapse;
        font-size: 0.85rem;
        min-width: 380px;
    }

    .printer-table.wide {
        width: 100%;
        min-width: 520px;
    }

    .printer-table th,
    .printer-table td {
        border: 1px solid var(--line-strong);
        padding: 6px 10px;
        text-align: left;
        vertical-align: top;
    }

    .printer-table th {
        background: var(--card);
        color: var(--muted);
        font-weight: 600;
    }

    .printer-table .nowrap {
        white-space: nowrap;
    }

    .two-col {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 240px), 1fr));
        gap: 12px;
    }

    .col-box {
        background: var(--card);
        border: 1px solid var(--line-strong);
        border-radius: 8px;
        padding: 10px 12px;
    }

    .col-box h4 {
        margin: 0 0 8px;
        color: var(--rp-foam);
        font-size: 0.95rem;
    }

    .mini-steps {
        padding-left: 18px;
        margin: 0;
    }

    .mini-steps li {
        margin-bottom: 6px;
        font-size: 0.86rem;
        line-height: 1.45;
    }

    .link-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 1fr));
        gap: 15px;
    }

    .guide-link {
        display: flex;
        align-items: flex-start;
        gap: 15px;
        padding: 16px 18px;
        background: var(--card);
        border: 2px solid var(--line-strong);
        border-radius: 8px;
        text-decoration: none;
        color: var(--rp-text);
        transition: transform var(--normal) var(--ease), border-color var(--normal) var(--ease), box-shadow var(--normal) var(--ease);
    }

    .guide-link:hover {
        border-color: var(--rp-foam);
        transform: translateY(-3px);
        box-shadow: 0 8px 16px color-mix(in srgb, var(--rp-foam) 20%, transparent);
    }

    .download-link {
        margin: 10px 0;
        border-color: color-mix(in srgb, var(--rp-foam) 45%, var(--line-strong));
    }

    .link-icon {
        font-size: 2rem;
        flex-shrink: 0;
        line-height: 1;
    }

    .link-content {
        min-width: 0;
    }

    .guide-section .link-content strong {
        color: var(--rp-foam);
        font-size: clamp(0.98rem, 2.2vw, 1.08rem);
        display: block;
        margin-bottom: 4px;
        overflow-wrap: anywhere;
    }

    .link-desc {
        display: block;
        color: var(--muted);
        font-size: clamp(0.85rem, 1.8vw, 0.93rem);
        line-height: 1.4;
    }

    .warning-section {
        border: 2px solid var(--rp-love);
        background: linear-gradient(135deg, color-mix(in srgb, var(--rp-love) 8%, var(--bg)), var(--bg));
    }

    .warning-section h3 {
        color: var(--rp-love);
    }

    .warning-list {
        display: flex;
        flex-direction: column;
        gap: 14px;
    }

    .warning-item {
        display: flex;
        align-items: flex-start;
        gap: 16px;
        padding: 16px 18px;
        background: var(--card);
        border-left: 4px solid var(--rp-love);
        border-radius: 8px;
    }

    .community-item {
        border-left-color: var(--rp-foam);
        background: color-mix(in srgb, var(--rp-foam) 8%, var(--card));
        align-items: center;
    }

    .warning-icon {
        font-size: 2rem;
        flex-shrink: 0;
        line-height: 1;
    }

    .warning-content strong {
        color: var(--rp-rose);
        font-size: clamp(1rem, 2.3vw, 1.1rem);
        display: block;
        margin-bottom: 6px;
    }

    .warning-content p {
        margin: 0;
        line-height: 1.55;
        font-size: clamp(0.9rem, 1.9vw, 0.98rem);
    }

    .contact-section {
        background: color-mix(in srgb, var(--rp-pine) 12%, var(--bg));
        border: 2px solid var(--rp-pine);
        text-align: center;
    }

    .contact-section h3 {
        color: var(--link);
    }

    .contact-section p {
        font-size: clamp(0.95rem, 2vw, 1.05rem);
        line-height: 1.7;
        margin: 0;
    }

    /* On Dawn, nudge coloured text towards the text colour so it reads */
    :global(:root[data-theme='light']) .info-chip,
    :global(:root[data-theme='light']) .category-tag.returned,
    :global(:root[data-theme='light']) .status-message.returned,
    :global(:root[data-theme='light']) .guide-section .link-content strong,
    :global(:root[data-theme='light']) .sub-head,
    :global(:root[data-theme='light']) .col-box h4 {
        color: color-mix(in srgb, var(--rp-foam) 70%, var(--rp-text));
    }

    :global(:root[data-theme='light']) .guide-section h3,
    :global(:root[data-theme='light']) .warning-content strong,
    :global(:root[data-theme='light']) .borrow-title {
        color: color-mix(in srgb, var(--rp-rose) 75%, var(--rp-text));
    }

    :global(:root[data-theme='light']) .steps strong {
        color: color-mix(in srgb, var(--rp-gold) 70%, var(--rp-text));
    }

    .item-photo a {
        display: inline-block;
    }

    @media (max-width: 768px) {
        .detail-row {
            flex-direction: column;
            gap: 8px;
        }

        .detail-item {
            min-width: auto;
        }

        .photo-thumbnail {
            max-width: 100%;
            max-height: 150px;
        }

        .return-action-btn,
        .return-cancel-btn {
            width: 100%;
        }

        .warning-item {
            gap: 12px;
            padding: 14px;
        }

        .warning-icon,
        .link-icon {
            font-size: 1.6rem;
        }
    }

    @media (max-width: 480px) {
        .form-container,
        .return-container,
        .printer-guide-container {
            padding: 16px 14px;
            border-radius: 10px;
        }

        .guide-section {
            padding: 14px 12px;
        }

        .steps,
        .plain-list {
            padding-left: 18px;
        }
    }
</style>
