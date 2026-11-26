<script>
    import { onDestroy, onMount } from 'svelte';
    import { browser } from '$app/environment';
    import { pushState, replaceState } from '$app/navigation';
    import { page } from '$app/stores';
    import { api, can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import { AREA } from '$lib/palette.js';
    import IconBorrow from '@tabler/icons-svelte-runes/icons/package-export';
    import IconReturn from '@tabler/icons-svelte-runes/icons/package-import';
    import IconVideo from '@tabler/icons-svelte-runes/icons/video';
    import IconRobot from '@tabler/icons-svelte-runes/icons/robot';
    import IconItems from '@tabler/icons-svelte-runes/icons/list-search';
    import IconBook from '@tabler/icons-svelte-runes/icons/book';
    import IconDashboard from '@tabler/icons-svelte-runes/icons/layout-dashboard';
    import IconThreads from '@tabler/icons-svelte-runes/icons/flask';
    import IconPrinter from '@tabler/icons-svelte-runes/icons/printer';
    import IconBox from '@tabler/icons-svelte-runes/icons/box';
    import IconArrowRight from '@tabler/icons-svelte-runes/icons/arrow-right';
    import IconAlert from '@tabler/icons-svelte-runes/icons/alert-triangle';
    import IconLock from '@tabler/icons-svelte-runes/icons/lock';
    import IconClock from '@tabler/icons-svelte-runes/icons/clock';
    import IconHome from '@tabler/icons-svelte-runes/icons/home';
    import IconPin from '@tabler/icons-svelte-runes/icons/map-pin';
    import IconSparkles from '@tabler/icons-svelte-runes/icons/sparkles';
    import IconCheck from '@tabler/icons-svelte-runes/icons/check';
    import IconCircleCheck from '@tabler/icons-svelte-runes/icons/circle-check';
    import IconExchange from '@tabler/icons-svelte-runes/icons/arrows-exchange';
    import IconSearch from '@tabler/icons-svelte-runes/icons/search';
    import IconX from '@tabler/icons-svelte-runes/icons/x';
    import IconInbox from '@tabler/icons-svelte-runes/icons/inbox';
    import IconCamera from '@tabler/icons-svelte-runes/icons/camera';
    import IconDownload from '@tabler/icons-svelte-runes/icons/download';
    import IconWifi from '@tabler/icons-svelte-runes/icons/wifi';
    import IconEye from '@tabler/icons-svelte-runes/icons/eye';
    import IconEyeOff from '@tabler/icons-svelte-runes/icons/eye-off';
    import IconExternal from '@tabler/icons-svelte-runes/icons/external-link';
    import IconInfo from '@tabler/icons-svelte-runes/icons/info-circle';
    import IconHelp from '@tabler/icons-svelte-runes/icons/help';
    import IconBan from '@tabler/icons-svelte-runes/icons/ban';
    import IconShield from '@tabler/icons-svelte-runes/icons/shield';
    import IconListCheck from '@tabler/icons-svelte-runes/icons/list-check';
    import IconUsers from '@tabler/icons-svelte-runes/icons/users';
    import IconPhone from '@tabler/icons-svelte-runes/icons/phone';
    import IconUser from '@tabler/icons-svelte-runes/icons/user';

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
        loadPulse();
        pulseTimer = setInterval(loadPulse, 30000);
        user = getUser();
        user = await refreshUser();
        ready = true;
        const wanted = new URLSearchParams(location.search).get('borrow');
        if (wanted !== null) borrowForm.item_name = wanted;
    });

    onDestroy(() => {
        clearInterval(pulseTimer);
        if (photoPreview) URL.revokeObjectURL(photoPreview);
    });

    // "The lab right now": a live line under the home heading, from the same
    // public data the printers, return and MoCap pages show. Each part stays
    // hidden until its request answers, so nothing flashes a wrong zero.
    let pulse = { printers: null, out: null, mocap: null };
    let pulseTimer;

    async function loadPulse() {
        const get = (url) => fetch(url).then((r) => (r.ok ? r.json() : null)).catch(() => null);
        const start = new Date();
        start.setHours(0, 0, 0, 0);
        const end = new Date(start.getTime() + 864e5);
        const [printers, loans, bookings] = await Promise.all([
            get('/api/printers'),
            get('/api/loans/active'),
            get(`/api/bookings?lab=mocap&from=${encodeURIComponent(start.toISOString())}&to=${encodeURIComponent(end.toISOString())}`)
        ]);
        pulse = {
            printers: Array.isArray(printers) && printers.length ? printerPulse(printers) : null,
            out: Array.isArray(loans) ? loans.length : null,
            mocap: Array.isArray(bookings) ? mocapPulse(bookings) : null
        };
    }

    function printerPulse(list) {
        const busy = (p) => ['RUNNING', 'PAUSE', 'PREPARE'].includes(p.state);
        const online = list.filter((p) => p.online);
        return {
            printing: online.filter(busy).length,
            free: online.filter((p) => !busy(p)).length,
            offline: list.length - online.length
        };
    }

    function mocapPulse(list) {
        const now = Date.now();
        const hhmm = (t) => new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        const sorted = [...list].sort((a, b) => new Date(a.start_time) - new Date(b.start_time));
        const current = sorted.find((b) => new Date(b.start_time) <= now && new Date(b.end_time) > now);
        if (current) return { busy: true, text: `MoCap in use until ${hhmm(current.end_time)}` };
        const next = sorted.find((b) => new Date(b.start_time) > now);
        if (next) return { busy: false, text: `MoCap free until ${hhmm(next.start_time)}` };
        return { busy: false, text: 'MoCap free for the rest of today' };
    }

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

    // The home page's action cards. Those with a `view` open in place (a
    // plain click is shallow-routed by go(); the href still works for a new tab).
    $: tiles = [
        { view: 'borrow', href: '/?view=borrow', title: 'Borrow', sub: 'Take equipment from the lab', icon: IconBorrow, color: 'pink' },
        { view: 'return', href: '/?view=return', title: 'Return', sub: 'Give equipment back', icon: IconReturn, color: 'azure' },
        { href: '/printers', title: '3D Printers', sub: 'See which printers are free', img: '/P1S.png', color: 'yellow' },
        { href: '/mocap', title: 'MoCap Lab', sub: 'Book a slot, see the calendar', icon: IconVideo, color: 'purple' },
        { href: '/mechlab', title: 'Mech Lab', sub: 'Book a robot', icon: IconRobot, color: 'red' },
        { href: '/items', title: "What's in the Lab", sub: 'Who has what, report a problem', icon: IconItems, color: 'teal' },
        { view: 'printer-guide', href: '/?view=printer-guide', title: 'Printer guidelines', sub: 'How to print, safely', icon: IconBook, color: 'orange' },
        ...(user
            ? [
                  { href: '/dashboard', title: 'Dashboard', sub: 'What needs you, your bookings', icon: IconDashboard, color: 'indigo' },
                  { href: '/threads', title: 'Research Threads', sub: 'Your projects and robot access', icon: IconThreads, color: 'green' }
              ]
            : [])
    ];

    $: firstName = (user?.name || '').trim().split(/\s+/)[0];

    // A plain left click opens a view in place; modified clicks (new tab)
    // fall through to the link.
    function openTile(e, tile) {
        if (!tile.view || e.button || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        e.preventDefault();
        go(tile.view);
    }

    // The printer guide's sections, for the in-page menu on wide screens
    const guideSections = [
        { id: 'guide-setup', label: '1. First-time setup' },
        { id: 'guide-presets', label: '2. Pick the right presets' },
        { id: 'guide-slice', label: '3. Slice and check' },
        { id: 'guide-send', label: '4. Send it to the printer' },
        { id: 'guide-during', label: '5. During and after' },
        { id: 'guide-stop', label: 'When to stop a print' },
        { id: 'guide-filament', label: 'Loading filament' },
        { id: 'guide-safety', label: 'Safety' },
        { id: 'guide-rules', label: 'Lab rules' },
        { id: 'guide-faq', label: 'Common questions' },
        { id: 'guide-links', label: 'Official guides' }
    ];

    // Scrolls without touching the URL, so the ?view= stays as it is
    function jumpTo(id) {
        document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
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

{#if message}
    <div class="toast-msg {messageType}" role="status">{message}</div>
{/if}

{#if currentView === 'home'}
    <div class="page-header d-print-none mt-0 mb-4">
        <div class="row g-3 align-items-center">
            <div class="col">
                <div class="page-pretitle">Robotics Research Centre</div>
                <h2 class="page-title">{firstName ? `Hello, ${firstName}` : 'Welcome to the lab'}</h2>
                <div class="text-secondary mt-1">Borrow and return equipment, book the labs and check on the printers.</div>
            </div>
            <div class="col-12 col-md-auto">
                <div class="btn-list">
                    <a class="btn btn-primary" href="/?view=borrow" on:click={(e) => openTile(e, tiles[0])}>
                        <IconBorrow size={18} class="icon" />Borrow
                    </a>
                    <a class="btn btn-outline-secondary" href="/?view=return" on:click={(e) => openTile(e, tiles[1])}>
                        <IconReturn size={18} class="icon" />Return
                    </a>
                </div>
            </div>
        </div>
    </div>

    <!-- The lab right now: each card appears once its request answers -->
    {#if pulse.printers || pulse.out !== null || pulse.mocap}
        <section class="row row-cards g-3 mb-3" aria-label="The lab right now">
            {#if pulse.printers}
                <div class="col-12 col-sm-6 col-lg-4">
                    <a class="card card-sm card-link h-100" href="/printers">
                        <div class="card-body">
                            <div class="row align-items-center g-3">
                                <div class="col-auto">
                                    <span class="avatar bg-yellow-lt"><IconPrinter size={22} class="icon" /></span>
                                </div>
                                <div class="col">
                                    <div class="fw-medium">
                                        <span class="tabular">{pulse.printers.free}</span> printer{pulse.printers.free === 1 ? '' : 's'} free
                                    </div>
                                    <div class="text-secondary small">
                                        {#if pulse.printers.printing > 0}<span class="status-dot status-dot-animated status-green me-1"></span>{/if}
                                        <span class="tabular">{pulse.printers.printing}</span> printing{#if pulse.printers.offline}, <span class="tabular">{pulse.printers.offline}</span> offline{/if}
                                    </div>
                                </div>
                            </div>
                        </div>
                    </a>
                </div>
            {/if}
            {#if pulse.out !== null}
                <div class="col-12 col-sm-6 col-lg-4">
                    <a class="card card-sm card-link h-100" href="/?view=return" on:click={(e) => openTile(e, tiles[1])}>
                        <div class="card-body">
                            <div class="row align-items-center g-3">
                                <div class="col-auto">
                                    <span class="avatar bg-azure-lt"><IconBox size={22} class="icon" /></span>
                                </div>
                                <div class="col">
                                    <div class="fw-medium"><span class="tabular">{pulse.out}</span> {pulse.out === 1 ? 'item' : 'items'} out</div>
                                    <div class="text-secondary small">Borrowed from the lab right now</div>
                                </div>
                            </div>
                        </div>
                    </a>
                </div>
            {/if}
            {#if pulse.mocap}
                <div class="col-12 col-sm-6 col-lg-4">
                    <a class="card card-sm card-link h-100" href="/mocap">
                        <div class="card-body">
                            <div class="row align-items-center g-3">
                                <div class="col-auto">
                                    <span class="avatar bg-purple-lt"><IconVideo size={22} class="icon" /></span>
                                </div>
                                <div class="col">
                                    <div class="fw-medium">
                                        <span class="status-dot me-1" class:status-red={pulse.mocap.busy} class:status-green={!pulse.mocap.busy}></span>
                                        {pulse.mocap.busy ? 'MoCap in use' : 'MoCap free'}
                                    </div>
                                    <div class="text-secondary small">{pulse.mocap.text}</div>
                                </div>
                            </div>
                        </div>
                    </a>
                </div>
            {/if}
        </section>
    {/if}

    {#if openMine.length}
        <section class="card mb-3" aria-labelledby="my-loans-title">
            <div class="card-status-top" style="background: {AREA.equipment}"></div>
            <div class="card-header">
                <h3 class="card-title" id="my-loans-title">You have</h3>
                <div class="card-actions">
                    <a class="btn btn-ghost-secondary btn-sm" href="/?view=return" on:click={(e) => openTile(e, tiles[1])}>All loans</a>
                </div>
            </div>
            <div class="list-group list-group-flush">
                {#each openMine as loan (loan.ID)}
                    <div class="list-group-item">
                        <div class="row align-items-center g-2">
                            <div class="col">
                                <span class="fw-medium">{loan.item_name}</span>{#if loan.quantity_borrowed > 1}<span class="text-secondary tabular"> x{loan.quantity_borrowed}</span>{/if}
                                <span
                                    class="badge ms-1"
                                    class:bg-red-lt={loan.overdue}
                                    class:bg-yellow-lt={!loan.overdue && loan.status === 'pending'}
                                    class:bg-secondary-lt={!loan.overdue && loan.status !== 'pending'}
                                >
                                    {dueText(loan)}
                                </span>
                            </div>
                            <div class="col-auto">
                                <button class="btn btn-outline-secondary" type="button" on:click={() => returnMine(loan)} disabled={loading}>
                                    {#if loan.status !== 'pending'}<IconCheck size={18} class="icon" />{/if}
                                    {loan.status === 'pending' ? 'Cancel request' : 'Returned'}
                                </button>
                            </div>
                        </div>
                    </div>
                {/each}
            </div>
        </section>
    {/if}

    <div class="row row-cards g-3">
        {#each tiles as tile (tile.href)}
            <div class="col-12 col-sm-6 col-lg-4 col-xxl-3">
                <a class="card card-link card-link-pop h-100" href={tile.href} on:click={(e) => openTile(e, tile)}>
                    <div class="card-body d-flex align-items-center gap-3">
                        {#if tile.img}
                            <span class="avatar avatar-md bg-{tile.color}-lt"><img class="tile-img" src={tile.img} alt="" /></span>
                        {:else}
                            <span class="avatar avatar-md bg-{tile.color}-lt"><svelte:component this={tile.icon} size={24} class="icon" /></span>
                        {/if}
                        <div class="flex-fill min-w-0">
                            <div class="fw-semibold">{tile.title}</div>
                            <div class="text-secondary small">{tile.sub}</div>
                        </div>
                        <IconArrowRight size={18} class="icon text-secondary flex-shrink-0" />
                    </div>
                </a>
            </div>
        {/each}
    </div>
{/if}

{#if currentView === 'borrow'}
    <div class="page-header d-print-none mt-0 mb-4">
        <div class="page-pretitle">Equipment</div>
        <h2 class="page-title">Borrow equipment</h2>
        <div class="text-secondary mt-1">Pick the item, say when it comes back, and you're done.</div>
    </div>

    <div class="row g-3">
        <div class="col-12 col-lg-8">
            <form class="card" on:submit|preventDefault={() => submitBorrow()}>
                <div class="card-body">
                    {#if user}
                        <div class="mb-3">
                            <div class="d-flex align-items-center justify-content-between mb-2">
                                <span class="form-label mb-0">Borrowing as</span>
                                <a class="small" href="/account">Not you?</a>
                            </div>
                            <div class="d-flex align-items-center gap-2 p-2 rounded border well">
                                <span class="avatar avatar-sm bg-primary-lt"><IconUser size={18} class="icon" /></span>
                                <span class="fw-medium">{user.name || ''}</span>
                                {#if user.phone}<span class="text-secondary tabular ms-auto">{user.phone}</span>{/if}
                            </div>
                            <div class="form-hint">From your account, so you only pick the item and the return date.</div>
                        </div>
                    {:else}
                        <div class="alert alert-info" role="note">
                            <div class="d-flex gap-2">
                                <IconInfo size={20} class="icon alert-icon flex-shrink-0" />
                                <div>Have an account? <a href={loginUrl()}>Log in</a>, then you only fill in the item and the date.</div>
                            </div>
                        </div>

                        <div class="row g-3 mb-3">
                            <div class="col-12 col-md-6">
                                <div class="d-flex align-items-center justify-content-between">
                                    <label class="form-label required" for="name">Name</label>
                                    {#if borrowForm.borrower_name}
                                        <button type="button" class="btn btn-link btn-sm p-0 mb-2" on:click={forgetContact}>Not you?</button>
                                    {/if}
                                </div>
                                <input
                                    type="text"
                                    id="name"
                                    class="form-control"
                                    bind:value={borrowForm.borrower_name}
                                    required
                                    maxlength="100"
                                    autocomplete="name"
                                    placeholder="Your name"
                                />
                                <div class="form-hint">Your name and phone are saved on this device for next time.</div>
                            </div>
                            <div class="col-12 col-md-6">
                                <label class="form-label required" for="phone">Phone number</label>
                                <input
                                    type="tel"
                                    id="phone"
                                    class="form-control"
                                    value={borrowForm.borrower_phone}
                                    required
                                    placeholder="10-digit phone number"
                                    title="Please enter exactly 10 digits"
                                    inputmode="numeric"
                                    autocomplete="tel-national"
                                    on:input={onPhoneInput}
                                />
                                <div class="form-hint">You'll need this number to return the item.</div>
                            </div>
                        </div>
                    {/if}

                    <div class="mb-3">
                        <label class="form-label required" for="item">Item</label>
                        <div class="input-icon">
                            <span class="input-icon-addon"><IconSearch size={18} class="icon" /></span>
                            <input
                                type="text"
                                id="item"
                                class="form-control"
                                list="catalogue"
                                bind:value={borrowForm.item_name}
                                required
                                autocomplete="off"
                                placeholder="Start typing, e.g. RealSense"
                            />
                        </div>
                        <datalist id="catalogue">
                            {#each items as item (item.ID)}
                                <option value={item.name}>{item.home_lab}{item.out.length ? ` · ${item.out.length} out` : ''}</option>
                            {/each}
                        </datalist>

                        {#if selectedItem}
                            <div class="mt-2 p-3 rounded border well">
                                <div class="d-flex flex-wrap gap-2">
                                    <span class="badge bg-secondary-lt"><IconPin size={14} class="icon me-1" />{selectedItem.home_lab || 'Lab not set'}</span>
                                    {#if freeCount !== null}
                                        <span
                                            class="badge"
                                            class:bg-red-lt={freeCount < borrowForm.quantity_borrowed}
                                            class:bg-green-lt={freeCount >= borrowForm.quantity_borrowed}
                                        >
                                            {freeCount} of {selectedItem.total_quantity} free
                                        </span>
                                    {/if}
                                    {#if holders.length}
                                        <span class="badge bg-azure-lt text-wrap text-start">Out with {holders.map((l) => l.borrower_name).join(', ')}</span>
                                    {/if}
                                    {#if waiting.length}
                                        <span class="badge bg-yellow-lt">{waiting.length} request{waiting.length === 1 ? '' : 's'} waiting</span>
                                    {/if}
                                </div>
                                {#if selectedItem.notes}
                                    <div class="form-hint mt-2">{selectedItem.notes}</div>
                                {/if}
                            </div>
                            {#if selectedItem.issue}
                                <div class="alert alert-warning mt-2 mb-0" role="note">
                                    <div class="d-flex gap-2">
                                        <IconAlert size={20} class="icon alert-icon flex-shrink-0" />
                                        <div>Reported problem: {selectedItem.issue}</div>
                                    </div>
                                </div>
                            {/if}
                            {#if selectedItem.lab_only}
                                <div class="alert alert-warning mt-2 mb-0" role="note">
                                    <div class="d-flex gap-2">
                                        <IconHome size={20} class="icon alert-icon flex-shrink-0" />
                                        <div>Lab only. Please don't take this out of the lab.</div>
                                    </div>
                                </div>
                            {/if}
                            {#if loginToRequest}
                                <div class="alert alert-danger mt-2 mb-0" role="note">
                                    <div class="d-flex gap-2">
                                        <IconLock size={20} class="icon alert-icon flex-shrink-0" />
                                        <div>Needs a lab admin's approval. <a href={loginUrl()}>Log in</a> to request it.</div>
                                    </div>
                                </div>
                            {:else if needsApproval}
                                <div class="alert alert-info mt-2 mb-0" role="note">
                                    <div class="d-flex gap-2">
                                        <IconClock size={20} class="icon alert-icon flex-shrink-0" />
                                        <div>Needs a lab admin's approval. You'll get a notification.</div>
                                    </div>
                                </div>
                            {/if}
                            {#if selectedItem.tracked}
                                <label class="form-check mt-3 mb-0">
                                    <input class="form-check-input" type="checkbox" bind:checked={borrowForm.accept} required />
                                    <span class="form-check-label">I take responsibility for this {selectedItem.name} until I return it or hand it on.</span>
                                </label>
                            {/if}
                        {:else if itemKey(borrowForm.item_name)}
                            <div class="form-hint"><IconSparkles size={14} class="icon me-1" />New item. It'll be added to the catalogue.</div>
                        {:else}
                            <div class="form-hint">Pick from the list, or type a new item's name to add it.</div>
                        {/if}
                    </div>

                    <div class="row g-3 mb-3">
                        {#if !selectedItem && itemKey(borrowForm.item_name)}
                            <div class="col-12 col-md-8">
                                <label class="form-label required" for="lab">Lab location</label>
                                <select id="lab" class="form-select" bind:value={borrowForm.lab_location} required>
                                    <option value="">Select the lab where the item lives</option>
                                    {#each labLocations as lab}
                                        <option value={lab.value}>{lab.label}</option>
                                    {/each}
                                </select>
                            </div>
                        {/if}
                        <div class="col-12 col-md-4">
                            <label class="form-label required" for="quantity">Quantity</label>
                            <input type="number" id="quantity" class="form-control" inputmode="numeric" bind:value={borrowForm.quantity_borrowed} min="1" required />
                        </div>
                        {#if freeCount !== null && borrowForm.quantity_borrowed > freeCount}
                            <div class="col-12 mt-1">
                                <div class="form-hint text-warning">The lab should only have {freeCount} free. You'll be asked to confirm.</div>
                            </div>
                        {/if}
                    </div>

                    <div class="mb-3">
                        <span class="form-label required" id="return-label">Return by</span>
                        <div class="btn-list" role="group" aria-labelledby="return-label">
                            {#each durationPresets as preset}
                                {@const on = !showCustomDuration && borrowForm.due_date === dateIn(preset.days)}
                                <button
                                    type="button"
                                    class="btn"
                                    class:btn-primary={on}
                                    class:btn-outline-secondary={!on}
                                    aria-pressed={on}
                                    on:click={() => { borrowForm.due_date = dateIn(preset.days); showCustomDuration = false; }}
                                >
                                    {preset.label}
                                </button>
                            {/each}
                            <button
                                type="button"
                                class="btn"
                                class:btn-primary={showCustomDuration}
                                class:btn-outline-secondary={!showCustomDuration}
                                aria-pressed={showCustomDuration}
                                on:click={() => (showCustomDuration = !showCustomDuration)}
                            >
                                Pick a date
                            </button>
                        </div>
                        {#if showCustomDuration}
                            <input
                                type="date"
                                class="form-control mt-2 date-input"
                                aria-label="Return date"
                                bind:value={borrowForm.due_date}
                                min={dateIn(0)}
                                max={dateIn(365)}
                                required
                            />
                        {/if}
                        <div class="form-hint">
                            Expected back by the end of <strong class="tabular">{shortDay(borrowForm.due_date)}</strong> ({daysAway(borrowForm.due_date)})
                        </div>
                    </div>

                    <div class="mb-3">
                        <label class="form-label" for="purpose">Purpose <span class="form-label-description">Optional</span></label>
                        <div class="btn-list mb-2">
                            {#each purposePresets as preset}
                                {@const on = borrowForm.purpose === preset}
                                <button
                                    type="button"
                                    class="btn btn-sm"
                                    class:btn-primary={on}
                                    class:btn-outline-secondary={!on}
                                    aria-pressed={on}
                                    on:click={() => pickPurpose(preset)}
                                >
                                    {preset}
                                </button>
                            {/each}
                        </div>
                        <textarea id="purpose" class="form-control" bind:value={borrowForm.purpose} placeholder="Anything more specific? (project name, priority)" rows="2"></textarea>
                    </div>

                    <div>
                        <label class="form-label" for="photo">Item photo <span class="form-label-description">Optional</span></label>
                        <input
                            type="file"
                            id="photo"
                            class="form-control"
                            accept="image/jpeg,image/jpg,image/png,image/webp,image/gif,image/bmp"
                            capture="environment"
                            bind:this={photoInput}
                            on:change={handlePhotoUpload}
                        />
                        <div class="form-hint">Take a photo or upload an image (JPG, PNG, WEBP, GIF, BMP, up to 10MB). It shows the condition the item left in.</div>
                        {#if borrowForm.item_photo}
                            <div class="d-flex align-items-center gap-3 mt-2 p-2 rounded border">
                                {#if photoPreview}
                                    <img class="photo-preview rounded" src={photoPreview} alt="Selected item" />
                                {/if}
                                <div class="flex-fill min-w-0 small">
                                    <div class="text-truncate fw-medium"><IconCamera size={16} class="icon me-1" />{borrowForm.item_photo.name}</div>
                                    <div class="text-secondary tabular">{(borrowForm.item_photo.size / 1024 / 1024).toFixed(2)}MB</div>
                                </div>
                                <button type="button" class="btn btn-ghost-danger" on:click={clearPhoto}>Remove</button>
                            </div>
                        {/if}
                    </div>
                </div>
                <div class="card-footer d-flex justify-content-end">
                    <button type="submit" class="btn btn-primary w-100 w-md-auto" disabled={isSubmitting}>
                        {#if isSubmitting}<span class="spinner-border spinner-border-sm me-2" role="status"></span>{/if}
                        {isSubmitting ? 'Processing...' : loginToRequest ? 'Log in to request' : needsApproval ? 'Request to borrow' : 'Borrow item'}
                    </button>
                </div>
            </form>
        </div>

        <div class="col-12 col-lg-4">
            <div class="card">
                <div class="card-header">
                    <h3 class="card-title">How borrowing works</h3>
                </div>
                <div class="card-body">
                    <ol class="steps steps-vertical steps-counter">
                        <li class="step-item">
                            <div class="fw-medium">Pick the item</div>
                            <div class="text-secondary small">Choose it from the list, or type a new item's name to add it.</div>
                        </li>
                        <li class="step-item">
                            <div class="fw-medium">Say when it comes back</div>
                            <div class="text-secondary small">It's due by the end of the day you pick.</div>
                        </li>
                        <li class="step-item">
                            <div class="fw-medium">Return it here</div>
                            <div class="text-secondary small">
                                Use <a href="/?view=return" on:click={(e) => openTile(e, tiles[1])}>Return</a>.
                                {user ? 'Your loans return in one tap.' : 'You return it with the phone number you borrowed with.'}
                            </div>
                        </li>
                    </ol>
                </div>
                <div class="card-footer text-secondary small">
                    Some items need a lab admin's approval first. Those can only be requested with an account.
                </div>
            </div>
        </div>
    </div>
{/if}

{#if currentView === 'return'}
    <div class="page-header d-print-none mt-0 mb-4">
        <div class="page-pretitle">Equipment</div>
        <h2 class="page-title">Return equipment</h2>
        <div class="text-secondary mt-1">
            Your own loans are at the top.{#if !user}{' '}Borrowed without an account? Return it with the phone number you used.{/if}
        </div>
    </div>

    <div class="card">
        <div class="card-header flex-wrap gap-2">
            <h3 class="card-title me-auto">
                {#if searchQuery.trim()}
                    Search results <span class="text-secondary tabular">({filteredLoans.length} found)</span>
                {:else}
                    All items <span class="text-secondary tabular">({filteredLoans.length} total)</span>
                {/if}
            </h3>
            <div class="search-box">
                <label class="visually-hidden" for="search">Search by name or item</label>
                <div class="input-group">
                    <span class="input-group-text"><IconSearch size={18} class="icon" /></span>
                    <input
                        type="text"
                        id="search"
                        class="form-control"
                        bind:value={searchQuery}
                        placeholder="Name or item"
                        autocomplete="off"
                    />
                    {#if searchQuery && searchQuery.trim()}
                        <button class="btn btn-icon" on:click={clearSearch} title="Clear search" aria-label="Clear search" type="button">
                            <IconX size={18} class="icon" />
                        </button>
                    {/if}
                </div>
            </div>
        </div>

        {#if loading && !filteredLoans.length}
            <div class="card-body text-center py-5">
                <div class="spinner-border text-secondary" role="status"></div>
                <div class="text-secondary mt-2">Loading items...</div>
            </div>
        {:else if filteredLoans.length === 0}
            <div class="empty">
                <div class="empty-icon"><IconInbox size={48} class="icon" /></div>
                <p class="empty-title">No items found</p>
                <p class="empty-subtitle text-secondary">
                    {#if searchQuery.trim()}
                        Try different search terms or clear the search to see all items.
                    {:else}
                        Nothing is out right now.
                    {/if}
                </p>
            </div>
        {:else}
            <div class="list-group list-group-flush">
                {#each filteredLoans as loan (loan.ID)}
                    {@const itemCategory = loan.status === 'not_found' ? 'missing' : loan.status === 'returned' ? 'returned' : 'borrowed'}
                    {@const late = itemCategory === 'borrowed' && !!loan.expected_return_date && isOverdue(loan.expected_return_date)}
                    {@const mine = isMine(loan)}

                    <div class="list-group-item" class:mine-row={mine}>
                        <div class="row g-3 align-items-center">
                            {#if loan.photo_filename}
                                <div class="col-auto">
                                    <a href="/api/photos/{loan.photo_filename}" target="_blank" rel="noopener noreferrer">
                                        <img src="/api/photos/{loan.photo_filename}" alt={loan.item_name} class="loan-photo rounded border" loading="lazy" />
                                    </a>
                                </div>
                            {/if}
                            <div class="col">
                                <div class="d-flex flex-wrap align-items-center gap-2 mb-1">
                                    <span class="fw-semibold">{loan.item_name}</span>
                                    {#if itemCategory === 'missing'}
                                        <span class="badge bg-red-lt">Missing</span>
                                    {:else if itemCategory === 'returned'}
                                        <span class="badge bg-green-lt">Returned</span>
                                    {:else if late}
                                        <span class="badge bg-orange-lt">Overdue</span>
                                    {:else}
                                        <span class="badge bg-blue-lt">Borrowed</span>
                                    {/if}
                                    {#if mine}
                                        <span class="badge bg-primary-lt">Yours</span>
                                    {:else if isProbablyMine(loan) && itemCategory === 'borrowed'}
                                        <span class="badge bg-primary-lt">Yours?</span>
                                    {/if}
                                </div>
                                <div class="text-secondary small loan-meta">
                                    <span>{loan.borrower_name}</span>
                                    <span>{loan.lab_location}</span>
                                    <span>Qty <span class="tabular">{loan.quantity_borrowed}</span></span>
                                    <span>Borrowed <span class="tabular">{formatDate(loan.CreatedAt)}</span></span>
                                    {#if loan.expected_return_date}
                                        <span class:text-danger={late}>Return due: {formatExpectedReturn(loan.expected_return_date)}</span>
                                    {/if}
                                </div>
                            </div>

                            <div class="col-12 col-md-auto">
                                {#if itemCategory === 'missing'}
                                    <div class="text-danger small">Marked missing by an admin. If you find it, tell lab staff.</div>
                                {:else if itemCategory === 'returned'}
                                    <div class="text-success small"><IconCircleCheck size={16} class="icon me-1" />Returned</div>
                                {:else if mine}
                                    <button class="btn btn-success w-100" type="button" on:click={() => returnMine(loan)} disabled={loading}>
                                        <IconCheck size={18} class="icon" />Mark as returned
                                    </button>
                                {:else if loan.user_id}
                                    <button class="btn btn-outline-primary w-100" type="button" on:click={() => takeOver(loan)} disabled={loading}>
                                        <IconExchange size={18} class="icon" />I have this now
                                    </button>
                                    <div class="form-hint handover-hint">Got it straight from {loan.borrower_name}? Move it to your account.</div>
                                {:else if returningLoanId !== loan.ID}
                                    <button class="btn btn-outline-success w-100" type="button" on:click={() => startReturn(loan.ID)} disabled={loading}>
                                        <IconCheck size={18} class="icon" />Mark as returned
                                    </button>
                                {/if}
                            </div>

                            {#if itemCategory === 'borrowed' && !mine && !loan.user_id && returningLoanId === loan.ID}
                                <div class="col-12">
                                    <form class="p-3 rounded border well" on:submit|preventDefault={() => returnItem(loan.ID)}>
                                        <label class="form-label" for="return-phone-{loan.ID}">Phone number you borrowed it with</label>
                                        <div class="row g-2">
                                            <div class="col-12 col-sm">
                                                <div class="input-icon">
                                                    <span class="input-icon-addon"><IconPhone size={18} class="icon" /></span>
                                                    <input
                                                        id="return-phone-{loan.ID}"
                                                        type="tel"
                                                        class="form-control"
                                                        class:is-invalid={returnError}
                                                        inputmode="numeric"
                                                        autocomplete="tel"
                                                        bind:value={returnPhone}
                                                        placeholder="10-digit phone number"
                                                    />
                                                </div>
                                                {#if returnError}
                                                    <div class="invalid-feedback d-block" role="alert">{returnError}</div>
                                                {/if}
                                            </div>
                                            <div class="col-6 col-sm-auto">
                                                <button type="submit" class="btn btn-success w-100" disabled={loading}>
                                                    <IconCheck size={18} class="icon" />Confirm return
                                                </button>
                                            </div>
                                            <div class="col-6 col-sm-auto">
                                                <button type="button" class="btn btn-outline-secondary w-100" on:click={cancelReturn}>Cancel</button>
                                            </div>
                                        </div>
                                    </form>
                                </div>
                            {/if}
                        </div>
                    </div>
                {/each}
            </div>
        {/if}
    </div>
{/if}

{#if currentView === 'printer-guide'}
    <div class="page-header d-print-none mt-0 mb-4">
        <div class="page-pretitle">3D printers</div>
        <h2 class="page-title">Printer guide</h2>
        <div class="text-secondary mt-1">How to print on the lab's Bambu Lab P1S printers, step by step.</div>
    </div>

    <div class="row g-4">
        <div class="col-lg-3 d-none d-lg-block">
            <nav class="guide-nav" aria-label="On this page">
                <div class="subheader mb-2">On this page</div>
                <div class="list-group list-group-transparent">
                    {#each guideSections as s (s.id)}
                        <button type="button" class="list-group-item list-group-item-action py-2" on:click={() => jumpTo(s.id)}>{s.label}</button>
                    {/each}
                </div>
            </nav>
        </div>

        <div class="col-12 col-lg-9 guide">
            <!-- The one thing everyone must remember, before anything else -->
            <div class="alert alert-warning mb-4" role="note">
                <div class="d-flex gap-2">
                    <IconAlert size={20} class="icon alert-icon flex-shrink-0" />
                    <div>
                        <h4 class="alert-title">The one rule: put your name in the file name.</h4>
                        <div>
                            Name every print <code>yourname_object</code>, for example
                            <code>srinath_bracket.3mf</code>. The <a href="/printers">printer page</a> shows
                            the file name, so everyone can see whose print is running. A print with no
                            name on it may be stopped when someone else needs the printer.
                        </div>
                    </div>
                </div>
            </div>

            <section class="card mb-4" id="guide-setup">
                <div class="card-header">
                    <h3 class="card-title">1. First-time setup</h3>
                </div>
                <div class="card-body">
                    <p class="text-secondary">Do this once on your laptop. Next time, skip straight to step 2.</p>
                    <ol class="steps steps-vertical steps-counter">
                        <li class="step-item">
                            <div class="fw-semibold">Install a slicer.</div>
                            <p class="mb-2">
                                It turns your 3D model into instructions the printer understands
                                ("slicing"). Either one works with our printers:
                            </p>
                            <div class="list-group">
                                <a class="list-group-item list-group-item-action d-flex align-items-center gap-2" href="https://bambulab.com/en/download/studio" target="_blank" rel="noopener noreferrer">
                                    <div class="flex-fill">
                                        <div class="fw-medium">Bambu Studio <span class="badge bg-green-lt ms-1">Recommended</span></div>
                                        <div class="text-secondary small">The RRC presets below are made for it. Download from bambulab.com</div>
                                    </div>
                                    <IconExternal size={18} class="icon text-secondary" />
                                </a>
                                <a class="list-group-item list-group-item-action d-flex align-items-center gap-2" href="https://www.orcaslicer.com/download/" target="_blank" rel="noopener noreferrer">
                                    <div class="flex-fill">
                                        <div class="fw-medium">OrcaSlicer</div>
                                        <div class="text-secondary small">Download from orcaslicer.com</div>
                                    </div>
                                    <IconExternal size={18} class="icon text-secondary" />
                                </a>
                            </div>
                        </li>
                        <li class="step-item">
                            <div class="fw-semibold">Download the RRC presets and import them.</div>
                            <p class="mb-2">They are the lab's tested settings for our printers and our filament.</p>
                            <a class="card card-sm card-link mb-2" href="/downloads/RRC_Bambu_P1S_Preset.bbscfg" download>
                                <div class="card-body d-flex align-items-center gap-3">
                                    <span class="avatar bg-primary-lt"><IconDownload size={20} class="icon" /></span>
                                    <div class="min-w-0">
                                        <div class="fw-medium text-break">RRC_Bambu_P1S_Preset.bbscfg</div>
                                        <div class="text-secondary small">Printer, print and filament presets for the lab's P1S printers</div>
                                    </div>
                                </div>
                            </a>
                            <p class="mb-0">
                                In Bambu Studio choose <em>File &gt; Import &gt; Import Configs…</em>, pick the
                                file, and restart Bambu Studio if the presets do not show up.
                            </p>
                        </li>
                        <li class="step-item">
                            <div class="fw-semibold">Join the printer network.</div>
                            <p class="mb-2">
                                The printers have their own network inside the lab. You cannot reach
                                them from wifi@iiith.
                            </p>
                            <div class="p-3 rounded border well">
                                <div class="datagrid wifi-grid">
                                    <div class="datagrid-item">
                                        <div class="datagrid-title"><IconWifi size={14} class="icon me-1" />Network</div>
                                        <div class="datagrid-content"><code>printers@rrc</code></div>
                                    </div>
                                    <div class="datagrid-item">
                                        <div class="datagrid-title">Password</div>
                                        <div class="datagrid-content d-flex align-items-center gap-2">
                                            {#if showWifiPassword}
                                                <code>printers@1234</code>
                                                <button type="button" class="btn btn-sm btn-ghost-secondary" on:click={() => (showWifiPassword = false)}>
                                                    <IconEyeOff size={16} class="icon" />Hide
                                                </button>
                                            {:else}
                                                <code>••••••••••••</code>
                                                <button type="button" class="btn btn-sm btn-ghost-secondary" on:click={() => (showWifiPassword = true)}>
                                                    <IconEye size={16} class="icon" />Show
                                                </button>
                                            {/if}
                                        </div>
                                    </div>
                                </div>
                                <div class="form-hint mt-2">Lab members only. Do not post it in group chats or share it outside the lab.</div>
                            </div>
                        </li>
                        <li class="step-item">
                            <div class="fw-semibold">Add the printers in Bambu Studio</div>
                            <p class="mb-2">
                                Only if you want to send prints from Bambu Studio itself. Use <em>LAN mode</em> and enter the
                                printer's IP address and access code. The access code is on the printer's
                                screen under <em>Settings &gt; Network</em>; it is not listed here on purpose.
                            </p>
                            <div class="card">
                                <div class="table-responsive">
                                    <table class="table table-vcenter card-table">
                                        <thead>
                                            <tr><th>Printer</th><th>Label on the machine</th><th>IP address</th></tr>
                                        </thead>
                                        <tbody>
                                            <tr><td>Printer 1</td><td class="font-monospace">3DP-01P-279</td><td><code>192.168.2.101</code></td></tr>
                                            <tr><td>Printer 2</td><td class="font-monospace">3DP-01P-112</td><td><code>192.168.2.105</code></td></tr>
                                            <tr><td>Printer 3</td><td class="font-monospace">3DP-01P-739</td><td><code>192.168.2.102</code></td></tr>
                                        </tbody>
                                    </table>
                                </div>
                            </div>
                        </li>
                    </ol>
                </div>
            </section>

            <section class="card mb-4" id="guide-presets">
                <div class="card-header">
                    <h3 class="card-title">2. Pick the right presets</h3>
                </div>
                <div class="card-body">
                    <p class="text-secondary">
                        In Bambu Studio, choose these three things before you slice. Getting them
                        right is most of what makes a print work.
                    </p>

                    <h4 class="mt-3">Printer</h4>
                    <p>Always <code>Bambu Lab P1S 0.4 nozzle</code>.</p>

                    <h4 class="mt-4">Print settings (process)</h4>
                    <div class="card mb-2">
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr><th>Preset</th><th>Use it for</th><th>What it does</th></tr>
                                </thead>
                                <tbody>
                                    <tr>
                                        <td class="fw-medium text-nowrap">RRC Printing Default</td>
                                        <td>Almost every print. Start here.</td>
                                        <td>0.2 mm layers, 20% gyroid infill, tree supports on, brim on the outside.</td>
                                    </tr>
                                    <tr>
                                        <td class="fw-medium text-nowrap">Ironing</td>
                                        <td>Parts with flat tops that should look smooth.</td>
                                        <td>Same as the default, plus a final pass that smooths the top surface. Takes longer.</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>
                    </div>
                    <p class="form-hint">
                        Supports are switched on in both. If your model has no overhangs, turn
                        supports off to save time and filament. The full preset names end in
                        <em>@BBL X1C</em>; that is normal, the P1S uses the same print settings.
                    </p>

                    <h4 class="mt-4">Filament</h4>
                    <p>
                        Pick the preset for the filament that is <strong>actually loaded</strong> in
                        the printer you will use. Check on the printer page or the printer's screen.
                    </p>
                    <div class="card mb-2">
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr><th>Loaded filament</th><th>Preset</th><th>Nozzle</th><th>Bed</th></tr>
                                </thead>
                                <tbody>
                                    <tr>
                                        <td>Numakers PLA+</td>
                                        <td><code>Numakers PLA +</code></td>
                                        <td class="text-nowrap">220 °C</td>
                                        <td>55 °C smooth plate, 65 °C textured plate</td>
                                    </tr>
                                    <tr>
                                        <td>Numakers PLA Silk</td>
                                        <td><code>Numakers PLA Silk</code></td>
                                        <td class="text-nowrap">230 °C</td>
                                        <td class="text-nowrap">55 °C</td>
                                    </tr>
                                    <tr>
                                        <td>Numakers ABS</td>
                                        <td><code>Numakers ABS ABS</code></td>
                                        <td class="text-nowrap">260 °C</td>
                                        <td class="text-nowrap">90 °C</td>
                                    </tr>
                                    <tr>
                                        <td>Numakers ABS, tall or thin parts</td>
                                        <td><code>Numakers ABS ABS … 0.8 Retract</code></td>
                                        <td class="text-nowrap">260 °C</td>
                                        <td class="text-nowrap">90 °C</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>
                    </div>
                    <p class="form-hint">
                        The temperatures are already in the presets; they are listed so you can
                        check them. The "0.8 Retract" version lifts the nozzle 0.8 mm when it moves,
                        so it does not knock over or scrape tall, thin ABS parts.
                    </p>
                    <div class="alert alert-danger" role="note">
                        <div class="d-flex gap-2">
                            <IconAlert size={20} class="icon alert-icon flex-shrink-0" />
                            <div>
                                Never print one material with another material's preset. The wrong
                                temperature can <strong>clog the nozzle</strong>, and a clog takes the printer
                                out of service until it is repaired.
                            </div>
                        </div>
                    </div>

                    <h4 class="mt-4">Printing ABS</h4>
                    <ul class="mb-0">
                        <li>Keep the printer's lid and door <strong>closed</strong> for the whole print. A draught makes ABS warp and lift off the bed.</li>
                        <li>ABS gives off fumes. Keep the room ventilated and do not lean over the printer while it prints.</li>
                        <li>Use a textured or engineering plate, not the smooth plate.</li>
                    </ul>
                </div>
            </section>

            <section class="card mb-4" id="guide-slice">
                <div class="card-header">
                    <h3 class="card-title">3. Slice and check</h3>
                </div>
                <div class="card-body">
                    <ol class="steps steps-vertical steps-counter">
                        <li class="step-item">
                            <strong>Set the plate type</strong> in Bambu Studio to the plate that is
                            on the printer (smooth or textured). The bed temperature depends on it.
                        </li>
                        <li class="step-item">
                            <strong>Slice</strong>, then look at the preview. Check the estimated
                            time and how much filament it needs, and make sure there is enough left
                            on the spool.
                        </li>
                        <li class="step-item">
                            <strong>First few prints?</strong> Show your settings to a lab admin before
                            you start. A 30-second check can save a failed 10-hour print.
                        </li>
                        <li class="step-item">
                            <strong>Export</strong> the sliced file and name it
                            <code>yourname_object</code>.
                        </li>
                    </ol>
                </div>
            </section>

            <section class="card mb-4" id="guide-send">
                <div class="card-header">
                    <h3 class="card-title">4. Send it to the printer</h3>
                </div>
                <div class="card-body">
                    <ol class="steps steps-vertical steps-counter">
                        <li class="step-item">
                            <strong>Check the printer is free.</strong> Open the
                            <a href="/printers">printer page</a>. Look at the <em>camera</em>, not only
                            the status: a printer can say "Finished" while the last person's print is
                            still on the plate.
                        </li>
                        <li class="step-item">
                            <strong>Send the file.</strong> Either:
                            <ul class="mt-1 mb-0">
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
                        <li class="step-item">
                            <strong>Go to the printer and check it</strong> before you start: the
                            plate is empty and clean, and the right filament is loaded with enough on
                            the spool. Then start the print on the printer's screen.
                        </li>
                    </ol>
                </div>
            </section>

            <section class="card mb-4" id="guide-during">
                <div class="card-header">
                    <h3 class="card-title">5. During and after the print</h3>
                </div>
                <div class="card-body">
                    <ol class="steps steps-vertical steps-counter">
                        <li class="step-item">
                            <strong>Watch the first layer.</strong> Stay until it is down: most
                            failures happen there.
                        </li>
                        <li class="step-item">
                            <strong>Check in regularly</strong> on the <a href="/printers">printer
                            page</a>. The camera shows whether it is still printing properly. Do not
                            start a print and forget about it.
                        </li>
                        <li class="step-item">
                            <strong>Collect your print soon after it finishes</strong> and clear the
                            plate, so the next person can start. Take your waste (purge blobs,
                            supports, brim) with you.
                        </li>
                    </ol>
                </div>
            </section>

            <section class="card mb-4" id="guide-stop">
                <div class="card-status-top bg-danger"></div>
                <div class="card-header">
                    <h3 class="card-title">When to stop a print</h3>
                </div>
                <div class="card-body">
                    <p>
                        Stop the print on the printer's screen <strong>straight away</strong> and tell
                        a lab admin if you see any of these. Anyone may stop a print that is going
                        wrong. It is always better to stop early than to damage the printer.
                    </p>
                </div>
                <div class="list-group list-group-flush">
                    <div class="list-group-item d-flex gap-3">
                        <span class="avatar avatar-sm bg-red-lt flex-shrink-0"><IconBan size={18} class="icon" /></span>
                        <div>
                            <div class="fw-medium">No plastic coming out of the nozzle</div>
                            <div class="text-secondary">Stop. The nozzle may be clogged. Do not try to clear it yourself.</div>
                        </div>
                    </div>
                    <div class="list-group-item d-flex gap-3">
                        <span class="avatar avatar-sm bg-red-lt flex-shrink-0"><IconAlert size={18} class="icon" /></span>
                        <div>
                            <div class="fw-medium">Tangled filament, or printing in mid-air ("spaghetti")</div>
                            <div class="text-secondary">Stop. Do not touch the hot nozzle or bed.</div>
                        </div>
                    </div>
                    <div class="list-group-item d-flex gap-3">
                        <span class="avatar avatar-sm bg-red-lt flex-shrink-0"><IconX size={18} class="icon" /></span>
                        <div>
                            <div class="fw-medium">Print came loose, or looks badly wrong</div>
                            <div class="text-secondary">Stop to save filament. Take a photo if you can.</div>
                        </div>
                    </div>
                    <div class="list-group-item d-flex gap-3">
                        <span class="avatar avatar-sm bg-red-lt flex-shrink-0"><IconCamera size={18} class="icon" /></span>
                        <div>
                            <div class="fw-medium">An error on the printer's screen</div>
                            <div class="text-secondary">Take a photo of the error <em>before</em> clearing it, then tell an admin.</div>
                        </div>
                    </div>
                    <div class="list-group-item d-flex gap-3 align-items-center">
                        <span class="avatar avatar-sm bg-secondary-lt flex-shrink-0"><IconUsers size={18} class="icon" /></span>
                        <div>If you stop someone else's print, tell them or a lab admin.</div>
                    </div>
                </div>
            </section>

            <section class="card mb-4" id="guide-filament">
                <div class="card-header">
                    <h3 class="card-title">Loading and unloading filament</h3>
                </div>
                <div class="card-body">
                    <p>
                        Always use the menu on the printer's screen. Never pull filament out cold or
                        force it: that is how nozzles get damaged.
                    </p>
                    <div class="row g-3">
                        <div class="col-12 col-md-6">
                            <div class="p-3 rounded border h-100">
                                <h4>Loading</h4>
                                <ol class="mb-0 ps-3">
                                    <li>Put the spool on the holder so it unwinds without tangling.</li>
                                    <li>Cut the end at an angle so it feeds in cleanly.</li>
                                    <li>Push it through the tube until it reaches the extruder.</li>
                                    <li>On the screen, choose <em>Filament &gt; Load</em> and pick the material.</li>
                                    <li>Wait until only the new colour comes out of the nozzle.</li>
                                </ol>
                            </div>
                        </div>
                        <div class="col-12 col-md-6">
                            <div class="p-3 rounded border h-100">
                                <h4>Unloading</h4>
                                <ol class="mb-0 ps-3">
                                    <li>On the screen, choose <em>Filament &gt; Unload</em>.</li>
                                    <li>Wait for the nozzle to heat up. Do not skip this.</li>
                                    <li>Let the printer pull it back, then pull gently. If it resists, stop and ask an admin.</li>
                                    <li>Clip the end into the spool so it does not unwind for the next person.</li>
                                </ol>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            <section class="card mb-4" id="guide-safety">
                <div class="card-header">
                    <h3 class="card-title"><IconShield size={20} class="icon me-2 text-warning" />Safety</h3>
                </div>
                <div class="card-body">
                    <ul class="mb-0 spaced">
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
                </div>
            </section>

            <section class="card mb-4" id="guide-rules">
                <div class="card-header">
                    <h3 class="card-title"><IconListCheck size={20} class="icon me-2 text-primary" />Lab rules</h3>
                </div>
                <div class="card-body">
                    <ul class="mb-0 spaced">
                        <li>Put your name in the file name. Every time.</li>
                        <li>Use the RRC presets. Do not change printer settings, calibration or firmware.</li>
                        <li>Ask before using filament that is not yours. If you are not sure whose it is, ask; do not assume it is lab stock.</li>
                        <li>Do not cancel or remove someone else's print unless it is failing (see above). Otherwise ask an admin.</li>
                        <li>Do not start a long print and disappear without telling anyone.</li>
                        <li>Keep the printer area clean: clear purge blobs and supports, put tools back.</li>
                        <li>If you break something, say so. Hiding it costs the lab more.</li>
                    </ul>
                </div>
            </section>

            <section class="card mb-4" id="guide-faq">
                <div class="card-header">
                    <h3 class="card-title"><IconHelp size={20} class="icon me-2 text-info" />Common questions</h3>
                </div>
                <div class="list-group list-group-flush">
                    <div class="list-group-item">
                        <div class="fw-medium">A printer shows Offline?</div>
                        <div class="text-secondary">It is switched off or off the network. Tell an admin.</div>
                    </div>
                    <div class="list-group-item">
                        <div class="fw-medium">Your print stopped and you do not know why?</div>
                        <div class="text-secondary">An admin may have stopped it; the printer page shows who. Ask them.</div>
                    </div>
                    <div class="list-group-item">
                        <div class="fw-medium">A printer says "Access code changed"?</div>
                        <div class="text-secondary">Nothing you did. Tell an admin so they can update it.</div>
                    </div>
                    <div class="list-group-item">
                        <div class="fw-medium">Your print is failing and you are not in the lab?</div>
                        <div class="text-secondary">Message a lab admin. Admins can stop any print from the printer page.</div>
                    </div>
                </div>
            </section>

            <section class="card mb-4" id="guide-links">
                <div class="card-header">
                    <h3 class="card-title">Official Bambu Lab guides</h3>
                </div>
                <div class="card-body">
                    <div class="row g-3">
                        {#each [
                            { href: 'https://wiki.bambulab.com/en/p1/manual/loading-filament', title: 'Loading filament', desc: 'Step-by-step, with pictures' },
                            { href: 'https://wiki.bambulab.com/en/p1/manual/print-from-bambu-studio', title: 'Printing from Bambu Studio', desc: 'Sending a print over the network' },
                            { href: 'https://wiki.bambulab.com/en/p1/manual', title: 'P1S manual', desc: 'Everything else about the printer' },
                            { href: 'https://wiki.bambulab.com/en/general/filament-guide-material-table', title: 'Filament guide', desc: 'Which material to use for what' }
                        ] as link (link.href)}
                            <div class="col-12 col-md-6">
                                <a class="card card-sm card-link h-100" href={link.href} target="_blank" rel="noopener noreferrer">
                                    <div class="card-body d-flex align-items-center gap-3">
                                        <span class="avatar bg-blue-lt"><IconBook size={20} class="icon" /></span>
                                        <div class="flex-fill min-w-0">
                                            <div class="fw-medium">{link.title}</div>
                                            <div class="text-secondary small">{link.desc}</div>
                                        </div>
                                        <IconExternal size={18} class="icon text-secondary" />
                                    </div>
                                </a>
                            </div>
                        {/each}
                    </div>
                </div>
            </section>

            <div class="alert alert-info mb-0" role="note">
                <div class="d-flex gap-2">
                    <IconHelp size={20} class="icon alert-icon flex-shrink-0" />
                    <div>
                        <h4 class="alert-title">Need help?</h4>
                        <div>Ask a lab admin. It is always better to stop and ask than to damage a printer.</div>
                    </div>
                </div>
            </div>
        </div>
    </div>
{/if}

<style>
    /* The P1S picture inside its avatar */
    .tile-img {
        width: 100%;
        height: 100%;
        object-fit: contain;
        padding: 3px;
    }

    .min-w-0 {
        min-width: 0;
    }

    /* Bootstrap has no w-md-auto */
    @media (min-width: 768px) {
        .w-md-auto {
            width: auto !important;
        }
    }

    .date-input {
        max-width: 14rem;
    }

    .photo-preview {
        width: 4rem;
        height: 4rem;
        object-fit: cover;
        flex-shrink: 0;
    }

    .loan-photo {
        width: 3.5rem;
        height: 3.5rem;
        object-fit: cover;
        display: block;
    }

    /* Your own loans stand out a little in the list */
    .mine-row {
        box-shadow: inset 3px 0 0 var(--tblr-primary);
    }

    /* Borrower, lab, quantity and dates on one wrapping line */
    .loan-meta {
        display: flex;
        flex-wrap: wrap;
        gap: 0.125rem 0.75rem;
    }

    .search-box {
        width: 100%;
        max-width: 20rem;
    }

    @media (max-width: 575.98px) {
        .search-box {
            max-width: none;
        }
    }

    .handover-hint {
        max-width: 16rem;
    }

    @media (max-width: 767.98px) {
        .handover-hint {
            max-width: none;
        }
    }

    /* Printer guide */
    .guide-nav {
        position: sticky;
        top: 1rem;
    }

    .guide section {
        scroll-margin-top: 1rem;
    }

    /* Tabler clips .steps for its horizontal form; the counters need room */
    .steps-vertical {
        overflow: visible;
    }

    .well {
        background: var(--tblr-bg-surface-secondary);
    }

    .wifi-grid {
        --tblr-datagrid-item-width: 12rem;
    }

    .spaced li + li {
        margin-top: 0.5rem;
    }
</style>
