<script>
    import { onMount, onDestroy, tick } from 'svelte';
    import { replaceState } from '$app/navigation';
    import { cameraFeed } from '$lib/camera.js';
    import { api, can, clearSession as clearAuth, getToken, loginUrl, refreshUser } from '$lib/auth.js';
    import BookingPage from '$lib/BookingPage.svelte';
    import { hueFor } from '$lib/palette.js';

    // Authentication state
    let isLoggedIn = false;
    let adminInfo = null;
    let authToken = '';

    // Current view
    // dashboard, lost-missing, lab-view, printers, bookings, approvals, history,
    // items, users, change-password
    let currentView = 'dashboard';
    let navEl;
    let selectedLab = '';
    let selectedFilter = 'all'; // all, borrowed, returned, not_found, archived

    // Data
    let lostMissingItems = [];
    let bookings = [];

    // Bookings: which lab's calendar is open, and the robots being managed
    let bookingLab = 'mocap';
    let resources = [];
    let people = [];
    let editingResource = null;
    // Catalogue upkeep: the items people have added while borrowing
    let catalogue = [];
    let editingItem = null;
    let mergeInto = {};
    let itemSearch = '';
    let itemFilter = 'all'; // all, out, issue, tracked, approval, lab_only
    const ITEM_FILTERS = {
        all: () => true,
        out: (i) => itemsOut(i) > 0,
        issue: (i) => !!i.issue,
        tracked: (i) => i.tracked,
        approval: (i) => i.needs_approval,
        lab_only: (i) => i.lab_only
    };
    $: shownItems = catalogue.filter(
        (i) => i.name.toLowerCase().includes(itemSearch.trim().toLowerCase()) && ITEM_FILTERS[itemFilter](i)
    );
    $: personName = (id) => people.find((p) => p.id === id)?.name || (id ? `#${id}` : 'None');
    const blankResource = { slug: '', name: '', lab: 'mech', requires_approval: false, requires_thread: true, max_hours: 4, max_future_bookings: 3, active: true };

    // Printer control, mirrored from the public page so admins never have to
    // leave the dashboard to stop a job or fix an access code.
    let printers = [];
    let printerTimer;
    let cameraTimer;
    // Latest good frame per printer; the P1S manages ~0.5 fps
    const camera = cameraFeed();
    let stoppingPrinter = '';
    let editingCode = '';
    let newCode = '';
    let savingCode = false;
    let historyItems = [];
    let labLoans = [];
    let loading = false;
    let message = '';
    let messageType = '';

    // Search functionality for history
    let historySearchQuery = '';
    let historyStatus = 'all'; // all, active, overdue, returned, not_found
    let filteredHistoryItems = [];

    // Simple reactive statement for history search
    $: updateFilteredHistoryItems(historyItems, historySearchQuery, historyStatus);

    function updateFilteredHistoryItems(currentHistoryItems, currentSearchQuery, status) {
        if (!currentHistoryItems) {
            filteredHistoryItems = [];
            return;
        }

        let itemsToFilter = currentHistoryItems.filter((item) => {
            if (status === 'all') return true;
            if (status === 'overdue') return item.status === 'active' && isOverdue(item.expected_return_date);
            return item.status === status;
        });

        if (currentSearchQuery && currentSearchQuery.trim()) {
            const query = currentSearchQuery.toLowerCase().trim();
            itemsToFilter = itemsToFilter.filter(item => {
                const borrowerName = (item.borrower_name || '').toLowerCase();
                const itemName = (item.item_name || '').toLowerCase();
                const borrowerPhone = (item.borrower_phone || '').toLowerCase();
                const labLocation = (item.lab_location || '').toLowerCase();
                const purpose = (item.purpose || '').toLowerCase();
                const status = (item.status || '').toLowerCase();
                const approvedBy = (item.approved_by || '').toLowerCase();
                
                return borrowerName.includes(query) || 
                       itemName.includes(query) || 
                       borrowerPhone.includes(query) ||
                       labLocation.includes(query) ||
                       purpose.includes(query) ||
                       status.includes(query) ||
                       approvedBy.includes(query);
            });
        }
        
        filteredHistoryItems = itemsToFilter;
    }

    // Clear history search function
    function clearHistorySearch(event) {
        if (event) {
            event.preventDefault();
            event.stopPropagation();
        }
        historySearchQuery = '';
        // Focus back on the search input after clearing
        setTimeout(() => {
            const searchInput = document.getElementById('history-search');
            if (searchInput) {
                searchInput.focus();
            }
        }, 50);
    }

    // Users & groups (server admins)
    let users = [];
    let groups = [];
    let allPermissions = [];
    let editingUser = null;
    let overrideDraft = {}; // perm -> 'grant' | 'revoke'; absent means the group default
    const PERM_LABELS = {
        borrow_items: 'Borrow equipment',
        book_mocap: 'Book the Mocap Lab',
        book_mech: 'Book Mech Lab robots',
        approve_mech_bookings: 'Approve robot bookings',
        approve_loans: 'Approve loans',
        create_threads: 'Create research threads',
        auto_approve: 'Own bookings and loans need no sign-off',
        upload_prints: 'Send files to printers',
        manage_inventory: 'Manage inventory and loans',
        manage_printers: 'Control printers',
        manage_bookings: 'Manage all bookings',
        manage_users: 'Manage users (server admin)'
    };
    const permLabel = (p) => PERM_LABELS[p] || p;
    $: draftPerms = [...allPermissions, ...Object.keys(overrideDraft).filter((p) => !allPermissions.includes(p))];
    $: pendingUsers = users.filter((u) => u.status === 'pending');
    let userSearch = '';
    let userFilter = 'all'; // all, active, disabled, extra
    $: activeUserCount = users.filter((u) => u.status === 'active').length;
    $: disabledUserCount = users.filter((u) => u.status === 'disabled').length;
    $: shownUsers = users.filter((u) => {
        if (u.status === 'pending') return false;
        if (userFilter === 'active' && u.status !== 'active') return false;
        if (userFilter === 'disabled' && u.status !== 'disabled') return false;
        if (userFilter === 'extra' && !(u.overrides && u.overrides.length)) return false;
        const q = userSearch.trim().toLowerCase();
        if (!q) return true;
        return [u.name, u.email, u.phone, groupName(u.group_id)].some((v) => (v || '').toLowerCase().includes(q));
    });

    // Loan and booking requests waiting for an approver
    let pendingLoans = [];
    let pendingBookings = [];
    let deciding = '';
    $: isApprover = can(adminInfo, 'approve_loans') || can(adminInfo, 'approve_mech_bookings');
    $: approvalCount =
        (can(adminInfo, 'approve_loans') ? pendingLoans.length : 0) +
        (can(adminInfo, 'approve_mech_bookings') ? pendingBookings.filter((b) => b.user_id !== adminInfo?.id).length : 0);

    // Change your own password
    let changePasswordForm = { old_password: '', new_password: '', confirm_password: '' };
    let savingPassword = false;

    // Lab options
    const labs = ['Main Lab', 'Mech Lab', 'Control Lab'];

    onMount(async () => {
        adminInfo = await refreshUser();
        if (!adminInfo) {
            location.href = loginUrl();
            return;
        }
        authToken = getToken();
        isLoggedIn = true;
        if (can(adminInfo, 'manage_inventory')) { loadLostMissingItems(); loadCatalogue(); }
        if (can(adminInfo, 'manage_users')) loadUsers(); // pending sign-ups badge and card
        if (can(adminInfo, 'approve_loans') || can(adminInfo, 'approve_mech_bookings')) loadApprovals(); // approvals badge
        const tab = new URLSearchParams(location.search).get('tab');
        openTab(tab || 'overview');
    });

    // Deep links: /admin?tab=users, ?tab=lab:Main Lab, ... Each tab needs the
    // same permission as its nav button; anything else opens the overview.
    function openTab(tab) {
        const inv = can(adminInfo, 'manage_inventory');
        if (tab === 'users' && can(adminInfo, 'manage_users')) showUsers();
        else if (tab === 'items' && inv) showItems();
        else if (tab === 'missing' && inv) showLostMissingItems();
        else if (tab === 'history' && inv) showItemHistory();
        else if (tab === 'bookings' && can(adminInfo, 'manage_bookings')) showBookings();
        else if (tab === 'printers' && can(adminInfo, 'manage_printers')) showPrinters();
        else if (tab === 'approvals' && (can(adminInfo, 'approve_loans') || can(adminInfo, 'approve_mech_bookings'))) showApprovals();
        else if (tab === 'password') showChangePasswordView();
        else if (tab?.startsWith('lab:') && inv && labs.includes(tab.slice(4))) showLabView(tab.slice(4));
        else goToDashboard();
    }

    const TAB_PARAM = { dashboard: 'overview', 'lost-missing': 'missing', 'change-password': 'password' };

    // On a phone the tab strip scrolls sideways: keep the open tab in view
    $: if (navEl && currentView) {
        tick().then(() => navEl?.querySelector('.tab-btn.active')?.scrollIntoView({ block: 'nearest', inline: 'nearest' }));
    }
    // Keep the URL on the open tab so it can be bookmarked and reloaded
    $: if (isLoggedIn) {
        const tab = currentView === 'lab-view' ? `lab:${selectedLab}` : TAB_PARAM[currentView] || currentView;
        replaceState(tab === 'overview' ? location.pathname : `?tab=${encodeURIComponent(tab)}`, {});
    }

    const ADMIN_PERMS = ['manage_inventory', 'manage_printers', 'manage_bookings', 'manage_users', 'approve_mech_bookings', 'approve_loans'];
    $: hasAdminArea = ADMIN_PERMS.some((p) => can(adminInfo, p));

    // Authenticated fetch. Sends the user back to log in on 401.
    async function apiFetch(path, options = {}) {
        let response;
        try {
            response = await api(path, options);
        } catch (e) {
            showMessage('Network error. Please try again.', 'error');
            return null;
        }

        if (response.status === 401) {
            clearSession();
            return null;
        }
        return response;
    }

    function clearSession() {
        stopPrinterPolling();
        clearAuth();
        location.href = loginUrl();
    }

    // Body of a response as JSON, or {} when it is not JSON. A 502 from Caddy
    // while the backend restarts, or a 413 from the proxy's upload limit, is
    // HTML or empty, and a bare response.json() would throw on it.
    async function readJSON(response) {
        return response.json().catch(() => ({}));
    }

    // What to tell the admin about a failed request: the backend's own message
    // when it sent one, otherwise something that says what actually happened.
    function apiError(response, body, fallback) {
        if (body && body.error) return body.error;
        switch (response.status) {
            case 413:
                return 'That file is too large to upload.';
            case 429:
                return 'Too many attempts. Wait a few minutes and try again.';
            case 502:
            case 503:
            case 504:
                return 'The server is not responding right now. Try again in a minute.';
        }
        return `${fallback} (HTTP ${response.status})`;
    }

    // Load missing items
    async function loadLostMissingItems() {
        loading = true;
        try {
            const response = await apiFetch('/api/admin/loans/lost-missing');
            if (response && response.ok) {
                lostMissingItems = await response.json();
            } else if (response) {
                showMessage('Failed to load missing items', 'error');
            }
        } finally {
            loading = false;
        }
    }

    // Load complete item history (all items chronologically)
    async function loadHistoryItems() {
        loading = true;
        try {
            const response = await apiFetch('/api/admin/loans/history');
            if (response && response.ok) {
                historyItems = await response.json();
            } else if (response) {
                showMessage('Failed to load item history', 'error');
            }
        } finally {
            loading = false;
        }
    }

    // Load loans by lab
    async function loadLabLoans(lab, filter = 'all') {
        loading = true;
        try {
            // Returns older than two weeks drop out of the lab lists; the
            // archive keeps them, for every lab at once
            const archived = filter === 'archived';
            const response = await apiFetch(archived
                ? '/api/admin/loans/archived'
                : `/api/admin/loans/by-lab/${encodeURIComponent(lab)}?status=${filter}`);
            if (response && response.ok) {
                const loans = await response.json();
                labLoans = archived ? loans.filter((l) => l.lab_location === lab) : loans;
                selectedLab = lab;
                selectedFilter = filter;
            } else if (response) {
                showMessage('Failed to load lab loans', 'error');
            }
        } finally {
            loading = false;
        }
    }

    // Extend loan
    async function extendLoan(loanId, days, hours) {
        loading = true;
        try {
            const response = await apiFetch(`/api/admin/loans/${loanId}/extend`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    extend_days: parseInt(days) || 0,
                    extend_hours: parseInt(hours) || 0
                })
            });

            if (response && response.ok) {
                showMessage('Loan extended successfully!', 'success');
                if (currentView === 'lab-view') {
                    loadLabLoans(selectedLab, selectedFilter);
                }
            } else if (response) {
                const error = await readJSON(response);
                showMessage(apiError(response, error, 'Failed to extend loan'), 'error');
            }
        } finally {
            loading = false;
        }
    }

    // Mark an item as missing - the admin could not find it in the lab
    async function markAsMissing(loanId, itemName) {
        if (!confirm(`Mark "${itemName}" as missing?\n\nIt will show up in the Missing Items list.`)) {
            return;
        }

        loading = true;
        try {
            const response = await apiFetch(`/api/admin/loans/${loanId}/mark-missing`, {
                method: 'POST'
            });

            if (response && response.ok) {
                showMessage('Item marked as missing', 'success');
                refreshCurrentView();
            } else if (response) {
                const error = await readJSON(response);
                showMessage(apiError(response, error, 'Failed to mark item as missing'), 'error');
            }
        } finally {
            loading = false;
        }
    }

    // Mark item as found (restore from missing status)
    async function markAsFound(loanId) {
        loading = true;
        try {
            const response = await apiFetch(`/api/admin/loans/${loanId}/mark-found`, {
                method: 'POST'
            });

            if (response && response.ok) {
                showMessage('Item marked as found and restored to borrowed', 'success');
                refreshCurrentView();
            } else if (response) {
                const error = await readJSON(response);
                showMessage(apiError(response, error, 'Failed to mark item as found'), 'error');
            }
        } finally {
            loading = false;
        }
    }

    // Reload whichever list is currently on screen
    function refreshCurrentView() {
        if (currentView === 'lost-missing') {
            loadLostMissingItems();
        } else if (currentView === 'lab-view') {
            loadLabLoans(selectedLab, selectedFilter);
        } else if (currentView === 'history') {
            loadHistoryItems();
        }
    }

    // Load every lab's bookings (from a week ago onwards)
    async function loadBookings() {
        loading = true;
        try {
            const from = new Date();
            from.setDate(from.getDate() - 7);
            // The admin route: the public one leaves out bookers' phone numbers
            const response = await apiFetch(`/api/admin/bookings?from=${from.toISOString()}`);
            if (response && response.ok) {
                bookings = await response.json();
            } else if (response) {
                showMessage('Failed to load bookings', 'error');
            }
        } finally {
            loading = false;
        }
    }

    let bookingFilter = 'upcoming'; // upcoming, pending, no-show, all
    $: shownBookings = bookings.filter((b) => {
        if (bookingFilter === 'upcoming') return new Date(b.end_time) >= new Date();
        if (bookingFilter === 'pending') return b.status === 'pending';
        if (bookingFilter === 'no-show') return b.no_show;
        return true;
    });

    async function clearNoShow(booking) {
        const response = await apiFetch(`/api/admin/bookings/${booking.ID}/clear-no-show`, { method: 'POST' });
        if (!response) return;
        const result = await readJSON(response);
        if (response.ok) {
            showMessage(`No-show cleared for ${booking.booked_by}`, 'success');
            calendarVersion++;
            loadBookings();
        } else {
            showMessage(apiError(response, result, 'Could not clear the no-show'), 'error');
        }
    }

    async function deleteBooking(booking) {
        if (!confirm(`Delete the ${resourceName[booking.resource_id] || 'lab'} booking by ${booking.booked_by}?`)) {
            return;
        }

        const response = await apiFetch(`/api/admin/bookings/${booking.ID}`, { method: 'DELETE' });
        if (!response) return;

        if (response.ok) {
            showMessage('Booking deleted', 'success');
            calendarVersion++; // the embedded calendar still shows it otherwise
            loadBookings();
        } else {
            const error = await readJSON(response);
            showMessage(apiError(response, error, 'Failed to delete booking'), 'error');
        }
    }

    // --- bookable resources (robots) ----------------------------------------

    // Admin list (includes inactive robots), so old bookings still get a name
    // The public list (active robots only) fills in for approvers who can't
    // read the admin one
    let publicResources = [];
    $: resourceName = {
        ...Object.fromEntries(publicResources.map((r) => [r.id, r.name])),
        ...Object.fromEntries(resources.map((e) => [e.resource.ID, e.resource.name]))
    };
    let calendarVersion = 0;

    async function loadResources() {
        const r = await apiFetch('/api/admin/resources');
        if (r?.ok) resources = await r.json();
    }

    function editResource(entry) {
        editingResource = { ...(entry ? entry.resource : blankResource) };
    }

    async function saveResource() {
        const r = editingResource;
        const response = await apiFetch(r.ID ? `/api/admin/resources/${r.ID}` : '/api/admin/resources', {
            method: r.ID ? 'PUT' : 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ...r, max_hours: Number(r.max_hours), max_future_bookings: Number(r.max_future_bookings) })
        });
        if (!response) return;
        const result = await readJSON(response);
        if (response.ok) {
            showMessage(`${r.name} saved`, 'success');
            editingResource = null;
            loadResources();
        } else {
            showMessage(apiError(response, result, 'Could not save'), 'error');
        }
    }

    // --- item catalogue -----------------------------------------------------

    async function loadCatalogue() {
        const response = await apiFetch('/api/items');
        if (response?.ok) catalogue = await response.json();
    }

    function showItems() {
        currentView = 'items';
        loadCatalogue();
        apiFetch('/api/admin/people').then(async (p) => { if (p?.ok) people = await p.json(); });
    }

    function itemsOut(item) {
        return (item.out || []).filter((l) => l.status === 'active').reduce((n, l) => n + (l.quantity_borrowed || 0), 0);
    }

    // Send an item request and reload the catalogue on success
    async function itemRequest(path, method, body, done, fallback) {
        const response = await apiFetch(path, {
            method,
            headers: body ? { 'Content-Type': 'application/json' } : undefined,
            body: body ? JSON.stringify(body) : undefined
        });
        if (!response) return false;
        const result = await readJSON(response);
        if (!response.ok) {
            showMessage(apiError(response, result, fallback), 'error');
            return false;
        }
        showMessage(done, 'success');
        loadCatalogue();
        return true;
    }

    async function saveItem() {
        const i = editingItem;
        const ok = await itemRequest(`/api/admin/items/${i.ID}`, 'PUT', {
            name: i.name,
            home_lab: i.home_lab,
            total_quantity: Number(i.total_quantity) || 0,
            tracked: i.tracked,
            custodian_id: i.tracked && i.custodian_id ? Number(i.custodian_id) : null,
            lab_only: i.lab_only,
            needs_approval: i.needs_approval,
            notes: i.notes
        }, `${i.name} saved`, 'Could not save item');
        if (ok) editingItem = null;
    }

    async function mergeItem(item) {
        const target = catalogue.find((c) => c.ID === Number(mergeInto[item.ID]));
        if (!target) return;
        if (!confirm(`Merge "${item.name}" into "${target.name}"? Its loans move to "${target.name}" and "${item.name}" is deleted.`)) return;
        if (await itemRequest(`/api/admin/items/${item.ID}/merge`, 'POST', { into_id: target.ID }, `Merged into ${target.name}`, 'Could not merge')) {
            delete mergeInto[item.ID];
            mergeInto = mergeInto;
        }
    }

    function deleteItem(item) {
        if (!confirm(`Delete "${item.name}" from the catalogue?`)) return;
        itemRequest(`/api/admin/items/${item.ID}`, 'DELETE', null, `${item.name} deleted`, 'Could not delete');
    }

    function clearIssue(item) {
        itemRequest(`/api/items/${item.ID}/issue`, 'POST', { issue: '' }, `Issue on ${item.name} cleared`, 'Could not clear issue');
    }

    // --- 3D printers ------------------------------------------------------

    let printersLoaded = false;

    async function loadPrinters() {
        const response = await apiFetch('/api/printers');
        if (response && response.ok) {
            printers = await response.json();
        }
        printersLoaded = true;
    }

    async function showPrinters() {
        currentView = 'printers';
        // Each online printer's file count shows on its card straight away
        loadPrinters().then(() => {
            for (const p of printers) {
                if (p.online && !uploadFiles[p.id]) loadPrinterFiles(p, true);
            }
        });
        loadPrintJobs();
        stopPrinterPolling();
        printerTimer = setInterval(() => { loadPrinters(); loadPrintJobs(); }, 5000);
        cameraTimer = setInterval(() => camera.refresh(printers), 2000);
    }

    function stopPrinterPolling() {
        clearInterval(printerTimer);
        clearInterval(cameraTimer);
        printerTimer = undefined;
        cameraTimer = undefined;
    }

    // Polling belongs to the printers view only. Every way of leaving it -
    // tabs, saving a password, logging out - changes currentView, so stop the
    // timers here rather than in each of those places.
    $: if (currentView !== 'printers') stopPrinterPolling();

    onDestroy(() => {
        stopPrinterPolling();
        camera.destroy();
        clearTimeout(messageTimer);
    });

    let printJobs = [];
    let busyPrinter = '';

    // Sending sliced files to a printer, so nobody has to switch wifi
    // Printer ids whose files panel is open; several can be open side by side
    let filesOpen = {};
    let uploadFiles = {};      // printer id -> files already on the printer
    let uploading = '';
    let uploadProgress = 0;

    // Starting a print. This is the only thing here that makes a machine move
    // on its own, so the form asks for the plate explicitly rather than
    // guessing, and the confirm step spells out what is about to happen.
    let startFor = '';
    let starting = '';
    let startFile = '';
    let startBed = 'textured_plate';
    let startPlate = 1;
    // 'external', or the flat AMS tray number as a string. Kept as one value so
    // the picker is a plain bind:value with no event plumbing.
    let startSpool = 'external';
    let startLevel = true;
    let startFlow = false;
    let startTimelapse = false;

    const BED_TYPES = [
        { value: 'textured_plate', label: 'Textured PEI' },
        { value: 'cool_plate', label: 'Cool Plate' },
        { value: 'high_temp_plate', label: 'High Temp' },
        { value: 'eng_plate', label: 'Engineering' },
        { value: 'supertack_plate', label: 'Cool Plate SuperTack' }
    ];

    // The firmware wants one flat, zero based tray number across every unit:
    // the first AMS holds 0-3, the second 4-7. The UI counts slots from 1
    // because that is what is printed on the machine.
    function globalSlot(unit, slot) {
        return unit.id * 4 + (slot.slot - 1);
    }

    function toggleStart(printer) {
        if (startFor === printer.id) {
            startFor = '';
            return;
        }
        startFor = printer.id;
        startFile = '';
        startPlate = 1;
        startLevel = true;
        startFlow = false;
        startTimelapse = false;

        // Default to the first loaded AMS slot, or the external spool when
        // there is no AMS fitted
        const slots = amsChoices(printer);
        startSpool = slots.length > 0 ? String(slots[0].index) : 'external';

        loadPrinterFiles(printer);
    }

    // Every loaded AMS tray, flattened for the picker
    function amsChoices(printer) {
        const choices = [];
        for (const unit of printer.ams || []) {
            for (const slot of unit.slots || []) {
                if (slot.empty) continue;
                choices.push({
                    index: globalSlot(unit, slot),
                    label: `${slot.material || 'Unknown'}`,
                    color: slot.color,
                    unit: unit.id + 1,
                    slot: slot.slot,
                    remain: slot.remain
                });
            }
        }
        return choices;
    }

    function canStart(printer) {
        return printer.online && ['IDLE', 'FINISH', 'FAILED'].includes(printer.state);
    }

    async function startPrint(printer) {
        if (!startFile) {
            showMessage('Choose a file to print', 'error');
            return;
        }

        const bed = BED_TYPES.find((b) => b.value === startBed)?.label || startBed;
        const useAMS = startSpool !== 'external';
        const slot = useAMS ? Number(startSpool) : 0;
        const chosen = amsChoices(printer).find((c) => c.index === slot);
        let spool = 'the external spool';
        if (useAMS) {
            spool = chosen
                ? `AMS ${chosen.unit} slot ${chosen.slot} — ${chosen.label}`
                : `AMS tray ${slot}`;
        }

        const ok = confirm(
            `Start ${startFile} on ${printer.name}?\n\n` +
            `Plate: ${bed}\nFilament: ${spool}\n\n` +
            `The printer will start moving. Check the plate is clear and the ` +
            `right one is fitted before continuing.`
        );
        if (!ok) return;

        starting = printer.id;
        try {
            const response = await apiFetch(`/api/admin/printers/${printer.id}/print`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    file_name: startFile,
                    plate: Number(startPlate) || 1,
                    bed_type: startBed,
                    use_ams: useAMS,
                    ams_slot: slot,
                    bed_levelling: startLevel,
                    flow_cali: startFlow,
                    timelapse: startTimelapse
                })
            });
            if (!response) return;

            const result = await readJSON(response);
            if (response.ok) {
                showMessage(`Started ${startFile} on ${printer.name}`, 'success');
                startFor = '';
                loadPrinters();
            } else {
                showMessage(apiError(response, result, 'The printer refused that job'), 'error');
            }
        } finally {
            starting = '';
        }
    }

    // Every printer command goes through here so the UI stays consistent
    async function printerCommand(printer, path, options, successMessage) {
        busyPrinter = printer.id + path;
        try {
            const response = await apiFetch(`/api/admin/printers/${printer.id}/${path}`, options);
            if (!response) return;

            const result = await readJSON(response);
            if (response.ok) {
                showMessage(successMessage || result.message, 'success');
                loadPrinters();
            } else {
                showMessage(apiError(response, result, 'The printer refused that command'), 'error');
            }
        } finally {
            busyPrinter = '';
        }
    }

    function pausePrint(printer) {
        printerCommand(printer, 'pause', { method: 'POST' }, `Paused ${printer.name}`);
    }

    function resumePrint(printer) {
        printerCommand(printer, 'resume', { method: 'POST' }, `Resumed ${printer.name}`);
    }

    function toggleLight(printer) {
        printerCommand(printer, 'light', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ on: !printer.light_on })
        });
    }

    async function toggleUpload(printer) {
        if (filesOpen[printer.id]) {
            filesOpen = { ...filesOpen, [printer.id]: false };
            return;
        }
        filesOpen = { ...filesOpen, [printer.id]: true };
        loadPrinterFiles(printer, true); // the open panel shows any error itself
    }

    let fileErrors = {};  // printer id -> why its file list could not be read
    let listingFiles = '';

    // quiet: a background refresh, so a failure shows on the card, not as a toast
    async function loadPrinterFiles(printer, quiet = false) {
        listingFiles = printer.id;
        try {
            const response = await apiFetch(`/api/printers/${printer.id}/files`);
            if (!response) return;
            if (response.ok) {
                uploadFiles = { ...uploadFiles, [printer.id]: await response.json() };
                fileErrors = { ...fileErrors, [printer.id]: '' };
            } else {
                const error = await readJSON(response);
                const text = apiError(response, error, 'Could not list the printer files');
                fileErrors = { ...fileErrors, [printer.id]: text };
                if (!quiet) showMessage(text, 'error');
            }
        } finally {
            if (listingFiles === printer.id) listingFiles = '';
        }
    }

    // The ▶ next to a file: open the start form with that file chosen
    async function printThisFile(printer, name) {
        if (startFor !== printer.id) toggleStart(printer);
        startFile = name;
        await tick();
        document.getElementById(`start-${printer.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }

    async function sendFile(printer, event) {
        const file = event.target.files?.[0];
        if (!file) return;

        if (!/\.(3mf|gcode)$/i.test(file.name)) {
            showMessage('Only .3mf and .gcode files can be sent to a printer', 'error');
            event.target.value = '';
            return;
        }

        uploading = printer.id;
        uploadProgress = 0;

        const form = new FormData();
        form.append('file', file);

        try {
            // XHR rather than fetch, because these files are big enough that a
            // progress bar matters
            const result = await new Promise((resolve) => {
                const xhr = new XMLHttpRequest();
                xhr.open('POST', `/api/printers/${printer.id}/files`);
                xhr.setRequestHeader('Authorization', `Bearer ${authToken}`);
                xhr.upload.onprogress = (e) => {
                    if (e.lengthComputable) {
                        uploadProgress = Math.round((e.loaded / e.total) * 100);
                    }
                };
                xhr.onload = () => {
                    let body = {};
                    try {
                        body = JSON.parse(xhr.responseText);
                    } catch (e) {
                        body = {};
                    }
                    resolve({ status: xhr.status, body });
                };
                xhr.onerror = () => resolve({ status: 0, body: {} });
                xhr.send(form);
            });

            if (result.status === 401) {
                clearSession();
                showMessage('Your session expired. Log in again.', 'error');
            } else if (result.status === 200) {
                showMessage(result.body.message || 'File sent to the printer', 'success');
                loadPrinterFiles(printer);
            } else if (result.status === 0) {
                showMessage('The upload was interrupted. Check your connection and try again.', 'error');
            } else {
                showMessage(apiError(result, result.body, 'The printer would not accept that file'), 'error');
            }
        } finally {
            uploading = '';
            uploadProgress = 0;
            event.target.value = '';
        }
    }

    let clearing = '';

    // Clearing the whole card, for when a term's worth of plates has filled it
    async function clearPrinterFiles(printer) {
        const files = uploadFiles[printer.id] || [];
        if (files.length === 0) {
            showMessage('There are no files to clear', 'error');
            return;
        }

        const ok = confirm(
            `Delete all ${files.length} file(s) from ${printer.name}?\n\n` +
            `This cannot be undone. Anything anyone has sent and not yet ` +
            `printed will be gone.`
        );
        if (!ok) return;

        clearing = printer.id;
        try {
            const response = await apiFetch(
                `/api/admin/printers/${printer.id}/files`, { method: 'DELETE' });
            if (!response) return;

            const result = await readJSON(response);
            if (response.ok) {
                showMessage(result.message, 'success');
            } else {
                showMessage(apiError(response, result, 'Could not clear the printer'), 'error');
            }
            loadPrinterFiles(printer);
        } finally {
            clearing = '';
        }
    }

    async function deletePrinterFile(printer, name) {
        if (!confirm(`Delete ${name} from ${printer.name}?`)) return;

        const response = await apiFetch(
            `/api/admin/printers/${printer.id}/files/${encodeURIComponent(name)}`,
            { method: 'DELETE' });
        if (!response) return;

        if (response.ok) {
            showMessage('File deleted from the printer', 'success');
            loadPrinterFiles(printer);
        } else {
            const error = await readJSON(response);
            showMessage(apiError(response, error, 'Could not delete the file'), 'error');
        }
    }

    function fileSize(bytes) {
        if (bytes > 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
        return `${Math.max(Math.round(bytes / 1024), 1)} KB`;
    }

    let jobPrinter = '';
    let showAllJobs = false;
    $: jobPrinters = [...new Set(printJobs.map((j) => j.printer_name).filter(Boolean))].sort();
    $: shownJobs = jobPrinter ? printJobs.filter((j) => j.printer_name === jobPrinter) : printJobs;

    async function loadPrintJobs() {
        const response = await apiFetch('/api/admin/print-jobs');
        if (response && response.ok) {
            printJobs = await response.json();
        }
    }

    function jobResult(job) {
        let text = (job.result || '').charAt(0).toUpperCase() + (job.result || '').slice(1);
        if (job.stopped_by) text += ` by ${job.stopped_by}`;
        if (job.result !== 'finished' && job.result !== 'running' && job.last_percent) text += ` at ${job.last_percent}%`;
        return text;
    }

    function jobDuration(job) {
        if (!job.ended_at) return 'running';
        const mins = Math.round((new Date(job.ended_at) - new Date(job.started_at)) / 60000);
        if (mins < 60) return `${mins} min`;
        return `${Math.floor(mins / 60)}h ${mins % 60}m`;
    }

    async function stopPrint(printer) {
        const job = printer.file_name || 'the current job';
        if (!confirm(`Stop ${job} on ${printer.name}?\n\nThis cannot be undone - the print will be cancelled.`)) {
            return;
        }

        stoppingPrinter = printer.id;
        try {
            const response = await apiFetch(`/api/admin/printers/${printer.id}/stop`, {
                method: 'POST'
            });
            if (!response) return;

            const result = await readJSON(response);
            if (response.ok) {
                showMessage(`Stop command sent to ${printer.name}`, 'success');
                loadPrinters();
            } else {
                showMessage(apiError(response, result, 'Could not stop the print'), 'error');
            }
        } finally {
            stoppingPrinter = '';
        }
    }

    async function savePrinterCode(printer) {
        if (!newCode.trim()) {
            showMessage('Enter the new access code from the printer screen', 'error');
            return;
        }

        savingCode = true;
        try {
            const response = await apiFetch(`/api/admin/printers/${printer.id}/access-code`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ access_code: newCode.trim() })
            });
            if (!response) return;

            const result = await readJSON(response);
            if (response.ok) {
                showMessage(`${printer.name}: access code updated, reconnecting...`, 'success');
                editingCode = '';
                newCode = '';
                setTimeout(loadPrinters, 4000);
                setTimeout(loadPrinters, 12000);
            } else {
                showMessage(apiError(response, result, 'Could not update the access code'), 'error');
            }
        } finally {
            savingCode = false;
        }
    }

    function printerIsPrinting(printer) {
        return printer.online && printer.state === 'RUNNING';
    }

    function printerStateLabel(printer) {
        if (!printer.online) return 'Offline';
        switch (printer.state) {
            case 'RUNNING': return 'Printing';
            case 'FINISH': return 'Finished';
            case 'FAILED': return 'Failed';
            case 'PAUSE': return 'Paused';
            case 'PREPARE': return 'Preparing';
            case 'SLICING': return 'Slicing';
            case 'IDLE': return 'Idle';
            default: return printer.state || 'Idle';
        }
    }

    function printerStateClass(printer) {
        if (!printer.online) return 'offline';
        switch (printer.state) {
            case 'RUNNING': return 'running';
            case 'FAILED': return 'failed';
            case 'PAUSE': return 'paused';
            case 'FINISH': return 'finished';
            default: return 'idle';
        }
    }

    function formatRemaining(minutes) {
        if (!minutes || minutes <= 0) return '';
        if (minutes < 60) return `${minutes} min left`;
        const hours = Math.floor(minutes / 60);
        const rest = minutes % 60;
        return rest ? `${hours}h ${rest}m left` : `${hours}h left`;
    }

    // Export data as CSV
    async function exportCSV(path = '/api/admin/export-csv', filename = 'robotics_research_centre_loans.csv') {
        try {
            const response = await apiFetch(path);
            if (response && response.ok) {
                const blob = await response.blob();
                const url = window.URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = filename;
                document.body.appendChild(a);
                a.click();
                document.body.removeChild(a);
                window.URL.revokeObjectURL(url);
                showMessage('Data exported successfully!', 'success');
            } else if (response) {
                showMessage('Failed to export data', 'error');
            }
        } catch (e) {
            showMessage('Failed to export data', 'error');
        }
    }

    // Helper functions
    let messageTimer;
    function showMessage(text, type) {
        message = text;
        messageType = type;
        // Restart the countdown, or an older message's timer cuts this one short
        clearTimeout(messageTimer);
        messageTimer = setTimeout(() => {
            message = '';
            messageType = '';
        }, 5000);
    }

    function formatDate(dateString) {
        return new Date(dateString).toLocaleDateString();
    }

    function formatExpectedReturn(dateString) {
        const deadline = dueDeadline(dateString);
        if (!deadline) return dateString || '—';
        const diffMs = deadline.getTime() - Date.now();
        const diffHours = Math.ceil(diffMs / (1000 * 60 * 60));
        const diffDays = Math.ceil(diffMs / (1000 * 60 * 60 * 24));
        const day = deadline.toLocaleDateString();

        if (diffMs < 0) {
            return `Overdue (${day})`;
        } else if (diffHours <= 24) {
            return `${diffHours} hour${diffHours !== 1 ? 's' : ''} (${day})`;
        } else {
            return `${diffDays} day${diffDays !== 1 ? 's' : ''} (${day})`;
        }
    }

    // An item is due at the END of its return date, so something due today is
    // not late yet. Matches the backend and the admin sort order.
    function dueDeadline(returnDate) {
        // A plain date means that day here, not midnight UTC
        const plain = /^(\d{4})-(\d{2})-(\d{2})$/.exec(returnDate || '');
        const due = plain ? new Date(+plain[1], +plain[2] - 1, +plain[3]) : new Date(returnDate);
        if (isNaN(due)) return null;
        due.setHours(23, 59, 59, 999);
        return due;
    }

    function isOverdue(returnDate) {
        const deadline = dueDeadline(returnDate);
        return deadline !== null && deadline < new Date();
    }

    async function copyToClipboard(text) {
        try {
            await navigator.clipboard.writeText(text);
            showMessage(`Copied: ${text}`, 'success');
        } catch (err) {
            // Fallback for older browsers
            const textArea = document.createElement('textarea');
            textArea.value = text;
            document.body.appendChild(textArea);
            textArea.select();
            try {
                document.execCommand('copy');
                showMessage(`Copied: ${text}`, 'success');
            } catch (fallbackErr) {
                showMessage('Failed to copy to clipboard', 'error');
            }
            document.body.removeChild(textArea);
        }
    }

    function showLostMissingItems() {
        currentView = 'lost-missing';
        loadLostMissingItems();
    }

    function showBookings() {
        currentView = 'bookings';
        loadBookings();
        loadResources();
    }

    function showItemHistory() {
        currentView = 'history';
        loadHistoryItems();
    }

    function showLabView(lab) {
        currentView = 'lab-view';
        selectedLab = lab; // set now so the tab and URL follow straight away
        selectedFilter = 'all'; // Reset filter when switching labs
        loadLabLoans(lab, 'all');
    }

    function filterLabLoans(filter) {
        selectedFilter = filter;
        loadLabLoans(selectedLab, filter);
    }

    function goToDashboard() {
        currentView = 'dashboard';
        stopPrinterPolling();
    }

    // Users & groups
    async function showUsers() {
        currentView = 'users';
        await loadUsers();
    }

    async function loadUsers() {
        const [u, g] = await Promise.all([apiFetch('/api/admin/users'), apiFetch('/api/admin/groups')]);
        if (!u?.ok || !g?.ok) {
            if (u && g) showMessage('Failed to load users', 'error');
            return;
        }
        users = await u.json();
        const body = await g.json();
        groups = body.groups;
        allPermissions = body.all_permissions;
    }

    $: groupName = (id) => groups.find((g) => g.id === id)?.name ?? '—';

    async function sendJSON(path, method, data, done) {
        const response = await apiFetch(path, {
            method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data)
        });
        if (!response) return false;
        const result = await readJSON(response);
        showMessage(response.ok ? done : apiError(response, result, 'Request failed'), response.ok ? 'success' : 'error');
        return response.ok;
    }

    async function updateUser(user, changes, done) {
        const ok = await sendJSON(`/api/admin/users/${user.id}`, 'PATCH', changes, done);
        if (ok) loadUsers();
        return ok;
    }

    // A refused change must be undone by hand: the reloaded data is unchanged,
    // so Svelte never rewrites the control.
    async function changeGroup(e, user) {
        if (!(await updateUser(user, { group_id: Number(e.target.value) }, 'Group changed'))) e.target.value = user.group_id;
    }

    function editOverrides(user) {
        editingUser = user;
        overrideDraft = Object.fromEntries(user.overrides.map((o) => [o.perm, o.allow ? 'grant' : 'revoke']));
    }

    async function saveOverrides() {
        const overrides = Object.entries(overrideDraft)
            .filter(([, v]) => v)
            .map(([perm, v]) => ({ perm, allow: v === 'grant' }));
        if (await sendJSON(`/api/admin/users/${editingUser.id}/permissions`, 'PUT', { overrides }, 'Permissions saved')) {
            editingUser = null;
            loadUsers();
        }
    }

    async function resetPassword(user) {
        const password = prompt(`New password for ${user.name} (at least 8 characters). Tell them to change it after logging in.`);
        if (password) sendJSON(`/api/admin/users/${user.id}/password`, 'POST', { password }, `Password reset for ${user.name}`);
    }

    async function toggleGroupPerm(e, group, perm) {
        const on = e.target.checked;
        const permissions = on ? [...group.permissions, perm] : group.permissions.filter((p) => p !== perm);
        if (!(await sendJSON(`/api/admin/groups/${group.id}/permissions`, 'PUT', { permissions }, `${group.name} updated`))) e.target.checked = !on;
        loadUsers();
    }

    // --- approvals ------------------------------------------------------

    function showApprovals() {
        currentView = 'approvals';
        loadApprovals();
    }

    async function loadApprovals() {
        const jobs = [];
        if (can(adminInfo, 'approve_loans')) {
            jobs.push(apiFetch('/api/loans/pending').then(async (r) => { if (r?.ok) pendingLoans = await r.json(); }));
        }
        if (can(adminInfo, 'approve_mech_bookings')) {
            jobs.push(apiFetch('/api/bookings/pending').then(async (r) => { if (r?.ok) pendingBookings = await r.json(); }));
            if (publicResources.length === 0) {
                jobs.push(apiFetch('/api/resources').then(async (r) => { if (r?.ok) publicResources = await r.json(); }));
            }
        }
        await Promise.all(jobs);
    }

    async function decide(kind, id, decision, label) {
        deciding = kind + id;
        try {
            const response = await apiFetch(`/api/${kind}s/${id}/${decision}`, { method: 'POST' });
            if (!response) return;
            const result = await readJSON(response);
            if (response.ok) {
                showMessage(`${label} ${decision === 'approve' ? 'approved' : 'declined'}`, 'success');
            } else {
                showMessage(apiError(response, result, 'Could not save the decision'), 'error');
            }
            loadApprovals(); // handled elsewhere meanwhile? the list catches up either way
        } finally {
            deciding = '';
        }
    }

    function decideLoan(loan, decision) {
        if (decision === 'reject' && !confirm(`Decline ${loan.borrower_name}'s request for ${loan.item_name}?`)) return;
        decide('loan', loan.ID, decision, `${loan.borrower_name}'s request for ${loan.item_name}`);
    }

    function decideBooking(booking, decision) {
        if (decision === 'reject' && !confirm(`Decline ${booking.booked_by}'s booking request?`)) return;
        decide('booking', booking.ID, decision, `${booking.booked_by}'s booking`);
    }

    // --- your own account -------------------------------------------------

    function showChangePasswordView() {
        currentView = 'change-password';
        changePasswordForm = { old_password: '', new_password: '', confirm_password: '' };
    }

    async function changePassword() {
        const f = changePasswordForm;
        if (f.new_password !== f.confirm_password) {
            showMessage('The new passwords do not match', 'error');
            return;
        }
        if (f.new_password.length < 8) {
            showMessage('Password must be at least 8 characters long', 'error');
            return;
        }
        savingPassword = true;
        try {
            const response = await apiFetch('/api/auth/change-password', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ old_password: f.old_password, new_password: f.new_password })
            });
            if (!response) return;
            const result = await readJSON(response);
            if (response.ok) {
                showMessage(result.message || 'Password changed', 'success');
                changePasswordForm = { old_password: '', new_password: '', confirm_password: '' };
                goToDashboard();
            } else {
                showMessage(apiError(response, result, 'Could not change the password'), 'error');
            }
        } finally {
            savingPassword = false;
        }
    }

    async function logout() {
        stopPrinterPolling();
        try {
            await api('/api/auth/logout', { method: 'POST' });
        } catch (e) {
            // offline: the session is dropped here regardless
        }
        clearAuth();
        location.href = '/';
    }

    async function deleteAllItems() {
        if (!can(adminInfo, 'manage_users')) return;

        if (!confirm('Delete all item data? This cannot be undone.')) {
            return;
        }

        const response = await apiFetch('/api/admin/delete-all-items', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ confirm_delete: true })
        });
        if (!response) return;

        const result = await readJSON(response);
        if (response.ok) {
            showMessage(`Successfully deleted ${result.deleted_count} items`, 'success');
        } else {
            showMessage(apiError(response, result, 'Failed to delete items'), 'error');
        }
    }

</script>

{#if message}
    <div class="message {messageType}" role="status">{message}</div>
{/if}

<div class="admin-container">
    {#if !isLoggedIn}
        <p class="no-items">Checking your account…</p>
    {:else if !hasAdminArea}
        <!-- Signed in, but nothing here is theirs to manage -->
        <div class="form-container">
            <div class="form-card no-access">
                <h2>🔒 No admin access</h2>
                <p>
                    Hi {adminInfo.name}. Your account{adminInfo.group ? ` (${adminInfo.group})` : ''} doesn't have any
                    admin permissions, so there is nothing to manage here.
                </p>
                <p>If you think you should have access, ask a server admin to change your group.</p>
                <div class="form-actions">
                    <a class="submit-btn as-link" href="/dashboard">Go to your dashboard</a>
                    <a class="cancel-btn as-link" href="/account">Your account</a>
                </div>
            </div>
        </div>
    {:else}
        <div class="dashboard">
            <!-- Header: who is signed in (the site header carries the logo) -->
            <div class="dashboard-header">
                <div class="dashboard-title">
                    <h1>🛡️ Admin Dashboard</h1>
                </div>
                <div class="admin-info">
                    <span>Welcome, <strong>{adminInfo.name}</strong></span>
                    {#if adminInfo.group}<span class="role-pill">{adminInfo.group}</span>{/if}
                    <button class="logout-btn" on:click={logout}>Logout</button>
                </div>
            </div>

            <!-- Navigation: one tab per section this person may use -->
            <nav class="nav-tabs" aria-label="Admin sections" bind:this={navEl}>
                <button class="tab-btn" class:active={currentView === 'dashboard'} on:click={goToDashboard}>📊 Dashboard</button>
                {#if can(adminInfo, 'manage_inventory')}
                    <button class="tab-btn" class:active={currentView === 'lost-missing'} on:click={showLostMissingItems}>
                        🔍 Missing Items
                        {#if lostMissingItems.length > 0}<span class="badge">{lostMissingItems.length}</span>{/if}
                    </button>
                    {#each labs as lab}
                        <button class="tab-btn" class:active={currentView === 'lab-view' && selectedLab === lab} on:click={() => showLabView(lab)}>🏭 {lab}</button>
                    {/each}
                {/if}
                {#if can(adminInfo, 'manage_printers')}
                    <button class="tab-btn" class:active={currentView === 'printers'} on:click={showPrinters}>🖨️ Printers</button>
                {/if}
                {#if can(adminInfo, 'manage_bookings')}
                    <button class="tab-btn" class:active={currentView === 'bookings'} on:click={showBookings}>📅 Bookings</button>
                {/if}
                {#if isApprover}
                    <button class="tab-btn" class:active={currentView === 'approvals'} on:click={showApprovals}>
                        ✅ Approvals
                        {#if approvalCount > 0}<span class="badge">{approvalCount}</span>{/if}
                    </button>
                {/if}
                {#if can(adminInfo, 'manage_inventory')}
                    <button class="tab-btn" class:active={currentView === 'history'} on:click={showItemHistory}>📚 Item History</button>
                    <button class="tab-btn" class:active={currentView === 'items'} on:click={showItems}>📦 Items</button>
                {/if}
                {#if can(adminInfo, 'manage_users')}
                    <button class="tab-btn" class:active={currentView === 'users'} on:click={showUsers}>
                        👥 Users &amp; Groups
                        {#if pendingUsers.length > 0}<span class="badge">{pendingUsers.length}</span>{/if}
                    </button>
                {/if}
                <button class="tab-btn" class:active={currentView === 'change-password'} on:click={showChangePasswordView}>🔑 Change Password</button>
            </nav>

            <!-- Dashboard View -->
            {#if currentView === 'dashboard'}
                <div class="dashboard-content">
                    <h2>System Overview</h2>
                    <div class="overview-cards">
                        {#if can(adminInfo, 'manage_users')}
                            <div class="card" class:attention={pendingUsers.length > 0}>
                                <h3>👥 Pending Sign-ups</h3>
                                <p class="stat-number">{pendingUsers.length}</p>
                                <button class="card-btn" on:click={showUsers}>Review</button>
                            </div>
                        {/if}
                        {#if isApprover}
                            <div class="card" class:attention={approvalCount > 0}>
                                <h3>✅ Approvals</h3>
                                <p class="stat-number">{approvalCount}</p>
                                <button class="card-btn" on:click={showApprovals}>Review</button>
                            </div>
                        {/if}
                        {#if can(adminInfo, 'manage_inventory')}
                            <div class="card">
                                <h3>🔍 Missing Items</h3>
                                <p class="stat-number">{lostMissingItems.length}</p>
                                <button class="card-btn" on:click={showLostMissingItems}>Review</button>
                            </div>
                            <div class="card">
                                <h3>📦 Item Catalogue</h3>
                                <p class="stat-number">{catalogue.length}</p>
                                <button class="card-btn" on:click={showItems}>Manage</button>
                            </div>
                        {/if}
                        {#if can(adminInfo, 'manage_printers')}
                            <div class="card">
                                <h3>🖨️ 3D Printers</h3>
                                <p>Live status, files on each printer, start and stop prints</p>
                                <button class="card-btn" on:click={showPrinters}>Manage</button>
                            </div>
                        {/if}
                        {#if can(adminInfo, 'manage_bookings')}
                            <div class="card">
                                <h3>📅 Bookings</h3>
                                <p>Mocap Lab and Mech Lab robots</p>
                                <button class="card-btn" on:click={showBookings}>Manage</button>
                            </div>
                        {/if}
                        {#if can(adminInfo, 'manage_inventory')}
                            {#each labs as lab}
                                <div class="card">
                                    <h3>🏭 {lab}</h3>
                                    <p>Manage borrowed items</p>
                                    <button class="card-btn" on:click={() => showLabView(lab)}>View</button>
                                </div>
                            {/each}
                        {/if}
                    </div>
                    {#if can(adminInfo, 'manage_inventory') || can(adminInfo, 'manage_bookings') || can(adminInfo, 'manage_printers')}
                        <div class="export-actions">
                            {#if can(adminInfo, 'manage_inventory')}
                                <button class="export-btn" on:click={() => exportCSV()}>📊 Export Loans (CSV)</button>
                            {/if}
                            {#if can(adminInfo, 'manage_bookings')}
                                <button class="export-btn" on:click={() => exportCSV('/api/admin/export-bookings-csv', 'motion_capture_lab_bookings.csv')}>🎥 Export Bookings (CSV)</button>
                            {/if}
                            {#if can(adminInfo, 'manage_printers')}
                                <button class="export-btn" on:click={() => exportCSV('/api/admin/export-print-jobs-csv', 'print_jobs.csv')}>🖨️ Export Print Log (CSV)</button>
                            {/if}
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Missing Items View -->
            {#if currentView === 'lost-missing'}
                <div class="loans-content">
                    <h2>🔍 Missing Items</h2>
                    <p class="subtitle-text">Items an admin could not find in the lab</p>
                    {#if loading}
                        <p>Loading missing items...</p>
                    {:else if lostMissingItems.length === 0}
                        <p class="no-items">No missing items.</p>
                    {:else}
                        <div class="loans-grid">
                            {#each lostMissingItems as loan}
                                <div class="loan-card lost-missing">
                                    <div class="loan-header">
                                        <div class="loan-title-section">
                                            <h3>{loan.item_name}</h3>
                                            {#if loan.photo_filename}
                                                <div class="item-image">
                                                    <img src="/api/photos/{loan.photo_filename}" alt={loan.item_name} on:error={(e) => (e.target.style.display = 'none')} />
                                                </div>
                                            {:else}
                                                <div class="item-image placeholder"><span>📷</span></div>
                                            {/if}
                                        </div>
                                        <div class="status-badges">
                                            <span class="status-badge not-found">Missing</span>
                                        </div>
                                    </div>
                                    <div class="loan-details">
                                        <p><strong>Borrower:</strong> {loan.borrower_name} <span class="acct-tag" class:guest={!loan.user_id} title={loan.user_id ? "Borrowed while signed in" : "Borrowed without an account: reach them on this phone number"}>{loan.user_id ? "👤 account" : "📵 no account"}</span></p>
                                        <p><strong>Phone:</strong> {#if loan.borrower_phone}<button class="clickable-phone" on:click={() => copyToClipboard(loan.borrower_phone)} title="Click to copy phone number">{loan.borrower_phone}</button>{:else}—{/if}</p>
                                        <p><strong>Lab:</strong> {loan.lab_location}</p>
                                        <p><strong>Quantity:</strong> {loan.quantity_borrowed}</p>
                                        <p><strong>Expected Return:</strong> {formatExpectedReturn(loan.expected_return_date)}</p>
                                        <p><strong>Status:</strong> Marked as missing{#if loan.approved_by} by {loan.approved_by}{/if}</p>
                                        <p><strong>Borrowed:</strong> {formatDate(loan.CreatedAt)}</p>
                                    </div>
                                    {#if loan.status === 'not_found'}
                                        <div class="found-section">
                                            <p class="found-notice">🔍 Item marked as missing</p>
                                            <div class="found-actions">
                                                <button class="found-btn" on:click={() => markAsFound(loan.ID)} disabled={loading}>✅ Mark as Found</button>
                                            </div>
                                        </div>
                                    {/if}
                                </div>
                            {/each}
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- 3D Printers View -->
            {#if currentView === 'printers'}
                <div class="loans-content">
                    <h2>🖨️ 3D Printers</h2>
                    <p class="subtitle-text">
                        Live status, refreshing every few seconds. Each printer keeps its own files.
                        <a class="calendar-link" href="/printers" target="_blank" rel="noopener">Open the full page →</a>
                    </p>

                    {#if printers.length === 0}
                        <p class="no-items">{printersLoaded ? 'No printers are configured.' : 'Loading printers...'}</p>
                    {:else}
                        <div class="printer-admin-grid">
                            {#each printers as printer, i (printer.id)}
                                {@const files = uploadFiles[printer.id]}
                                <div class="printer-admin-card {printerStateClass(printer)}" style="--i: {i}">
                                    <div class="pa-head">
                                        <img src="/P1S.png" alt="" class="pa-image" />
                                        <div class="pa-title">
                                            <h3>{printer.name}</h3>
                                            <span class="pa-avail {printerIsPrinting(printer) ? 'busy' : 'free'}" class:unknown={!printer.online}>
                                                {printer.online ? (printerIsPrinting(printer) ? 'In use' : 'Free') : 'Unknown'}
                                            </span>
                                        </div>
                                        <span class="pa-state {printerStateClass(printer)}">{printerStateLabel(printer)}</span>
                                    </div>

                                    <div class="pa-camera">
                                        {#if printer.camera_online && $camera[printer.id]}
                                            <img src={$camera[printer.id]} alt="Camera view of {printer.name}" />
                                        {:else}
                                            <div class="pa-camera-empty">
                                                <img src="/P1S.png" alt="" />
                                                <p>{printer.camera_online ? 'Loading camera…' : 'No camera image'}</p>
                                            </div>
                                        {/if}
                                    </div>

                                    {#if printer.faults && printer.faults.length > 0}
                                        <div class="pa-faults">
                                            <strong>⚠️ Printer reported {printer.faults.length} fault{printer.faults.length > 1 ? 's' : ''}</strong>
                                            {#each printer.faults as fault}
                                                <a class="pa-fault {fault.severity}" href={fault.url} target="_blank" rel="noopener">
                                                    <span class="pa-fault-sev">{fault.severity}</span>
                                                    <span class="pa-fault-code">{fault.code}</span>
                                                    <span class="pa-fault-link">look up →</span>
                                                </a>
                                            {/each}
                                        </div>
                                    {/if}

                                    {#if printer.access_code_problem}
                                        <div class="pa-warning">
                                            <strong>⚠️ Access code changed</strong>
                                            <p>{printer.name} is refusing our access code - LAN mode was probably toggled.</p>
                                            {#if editingCode === printer.id}
                                                <div class="pa-code-form">
                                                    <input type="text" bind:value={newCode} placeholder="New access code" autocomplete="off" aria-label="New access code for {printer.name}" />
                                                    <button class="pa-save" on:click={() => savePrinterCode(printer)} disabled={savingCode}>{savingCode ? 'Saving...' : 'Save'}</button>
                                                    <button class="pa-cancel" on:click={() => (editingCode = '')}>Cancel</button>
                                                </div>
                                            {:else}
                                                <button class="pa-fix" on:click={() => { editingCode = printer.id; newCode = ''; }}>Update access code</button>
                                            {/if}
                                        </div>
                                    {/if}

                                    {#if printer.online}
                                        {#if printerIsPrinting(printer)}
                                            <div class="pa-progress-row">
                                                <div class="pa-progress-bar">
                                                    <div class="pa-progress-fill" style="width: {printer.progress}%"></div>
                                                </div>
                                                <span class="pa-progress-text">{printer.progress}%</span>
                                            </div>
                                            {#if printer.remaining_minutes > 0}
                                                <p class="pa-remaining">{formatRemaining(printer.remaining_minutes)}</p>
                                            {/if}
                                        {/if}

                                        {#if printer.file_name}
                                            <p class="pa-file" title={printer.file_name}>📄 {printer.file_name}</p>
                                        {/if}

                                        {#if (printer.ams && printer.ams.length > 0) || printer.external_spool}
                                            <div class="pa-ams">
                                                {#each printer.ams || [] as unit}
                                                    <div class="pa-ams-head">
                                                        <span class="pa-ams-title">AMS {unit.id + 1}</span>
                                                        {#if unit.humidity}
                                                            <span class="pa-ams-meta" title="The AMS reports humidity on its own 1-5 scale">💧 {unit.humidity}/5</span>
                                                        {/if}
                                                        {#if unit.temp}
                                                            <span class="pa-ams-meta">🌡️ {unit.temp}°C</span>
                                                        {/if}
                                                    </div>

                                                    <div class="pa-ams-slots">
                                                        {#each unit.slots || [] as slot}
                                                            <div
                                                                class="pa-ams-slot"
                                                                class:empty={slot.empty}
                                                                class:active={slot.active}
                                                                title={slot.empty
                                                                    ? `Slot ${slot.slot}: empty`
                                                                    : `Slot ${slot.slot}: ${slot.material}${slot.remain >= 0 ? ', ' + slot.remain + '% left' : ', amount unknown'}${slot.active ? ' (in use)' : ''}`}
                                                            >
                                                                <span class="pa-ams-top">
                                                                    <span class="pa-ams-swatch" style="background: {slot.color || 'transparent'}"></span>
                                                                    <span class="pa-ams-num">{slot.slot}</span>
                                                                    {#if slot.active}<span class="pa-ams-dot" title="Currently feeding"></span>{/if}
                                                                </span>
                                                                <span class="pa-ams-mat">{slot.empty ? 'Empty' : slot.material}</span>
                                                                {#if slot.remain >= 0}
                                                                    <span class="pa-ams-bar"><span class="pa-ams-fill" style="width: {slot.remain}%"></span></span>
                                                                    <span class="pa-ams-pct">{slot.remain}%</span>
                                                                {:else if !slot.empty}
                                                                    <span class="pa-ams-pct unknown" title="No RFID tag, so the printer cannot tell">? left</span>
                                                                {/if}
                                                            </div>
                                                        {/each}
                                                    </div>
                                                {/each}

                                                {#if printer.external_spool && !printer.external_spool.empty}
                                                    <div class="pa-ams-ext">
                                                        <span class="pa-ams-swatch" style="background: {printer.external_spool.color || 'transparent'}"></span>
                                                        <span class="pa-ams-ext-label">External spool</span>
                                                        <span class="pa-ams-ext-mat">{printer.external_spool.material}</span>
                                                        {#if printer.external_spool.remain >= 0}
                                                            <span class="pa-ams-pct">{printer.external_spool.remain}%</span>
                                                        {/if}
                                                    </div>
                                                {/if}
                                            </div>
                                        {/if}

                                        <div class="pa-temps">
                                            <span title="Nozzle">🔥 {printer.nozzle_temp.toFixed(0)}°C</span>
                                            <span title="Bed">🛏️ {printer.bed_temp.toFixed(0)}°C</span>
                                            {#if printer.chamber_temp > 0}
                                                <span title="Chamber">📦 {printer.chamber_temp.toFixed(0)}°C</span>
                                            {/if}
                                        </div>

                                        <div class="pa-actions">
                                            <button
                                                class="pa-light"
                                                class:on={printer.light_on}
                                                aria-pressed={printer.light_on}
                                                on:click={() => toggleLight(printer)}
                                                disabled={busyPrinter === printer.id + 'light'}
                                                title="Chamber light - the camera is useless in the dark"
                                            >{printer.light_on ? '💡 Light on' : '🌑 Light off'}</button>

                                            {#if printerIsPrinting(printer)}
                                                <button class="pa-pause" on:click={() => pausePrint(printer)} disabled={busyPrinter === printer.id + 'pause'}>⏸ Pause</button>
                                            {:else if printer.state === 'PAUSE'}
                                                <button class="pa-resume" on:click={() => resumePrint(printer)} disabled={busyPrinter === printer.id + 'resume'}>▶ Resume</button>
                                            {/if}
                                        </div>

                                        {#if printerIsPrinting(printer) || printer.state === 'PAUSE'}
                                            <button class="pa-stop" on:click={() => stopPrint(printer)} disabled={stoppingPrinter === printer.id}>
                                                {stoppingPrinter === printer.id ? 'Stopping...' : '⏹ Stop Print'}
                                            </button>
                                        {/if}
                                    {:else}
                                        <p class="pa-offline">Not responding - switched off or off the network. Files can only be listed while it is on.</p>
                                    {/if}

                                        <!-- This printer's own files: a separate box per printer -->
                                        <button class="pa-send-toggle" aria-expanded={filesOpen[printer.id]} on:click={() => toggleUpload(printer)}>
                                            {#if filesOpen[printer.id]}
                                                ✕ Close files
                                            {:else}
                                                📁 Files on this printer{files ? ` (${files.length})` : ''} · 📤 Send a file
                                            {/if}
                                        </button>

                                        {#if filesOpen[printer.id]}
                                            <div class="pa-send">
                                                <div class="pa-send-head">
                                                    <span class="pa-send-title">📁 {printer.name} · files</span>
                                                    <button class="pa-refresh" on:click={() => loadPrinterFiles(printer, true)} disabled={listingFiles === printer.id} title="Reload the list from the printer">
                                                        {listingFiles === printer.id ? 'Loading…' : '🔄 Refresh'}
                                                    </button>
                                                </div>
                                                <p class="pa-send-hint">
                                                    File names should start with the owner's name, e.g.
                                                    <code>srinath_bracket.3mf</code>. The file lands on
                                                    <strong>{printer.name}</strong> only - start it below or from the printer's screen once the plate is clear.
                                                </p>

                                                <label class="pa-file-pick">
                                                    <input type="file" accept=".3mf,.gcode" on:change={(e) => sendFile(printer, e)} disabled={uploading === printer.id} />
                                                    <span>{uploading === printer.id ? `Sending... ${uploadProgress}%` : '📤 Choose a .3mf or .gcode file'}</span>
                                                </label>

                                                {#if uploading === printer.id}
                                                    <div class="pa-upload-bar"><div class="pa-upload-fill" style="width: {uploadProgress}%"></div></div>
                                                {/if}

                                                {#if fileErrors[printer.id]}
                                                    <p class="pa-send-error">⚠️ {fileErrors[printer.id]}</p>
                                                {/if}

                                                {#if !files}
                                                    {#if !fileErrors[printer.id]}<p class="pa-send-empty">Loading the file list…</p>{/if}
                                                {:else if files.length === 0}
                                                    <p class="pa-send-empty">No printable files on this printer.</p>
                                                {:else}
                                                    <p class="pa-file-summary">{files.length} file{files.length === 1 ? '' : 's'} · {fileSize(files.reduce((n, f) => n + (f.size || 0), 0))}</p>
                                                    <ul class="pa-file-list">
                                                        {#each files as file (file.name)}
                                                            <li>
                                                                <span class="pa-file-icon" aria-hidden="true">{file.name.toLowerCase().endsWith('.gcode') ? '🧾' : '🧊'}</span>
                                                                <span class="pa-file-main">
                                                                    <span class="pa-file-name" title={file.name}>{file.name}</span>
                                                                    <span class="pa-file-size">{fileSize(file.size)}{#if file.time} · {file.time}{/if}</span>
                                                                </span>
                                                                {#if canStart(printer)}
                                                                    <button class="pa-file-btn print" on:click={() => printThisFile(printer, file.name)} title="Start {file.name} on {printer.name}" aria-label="Print {file.name}">▶</button>
                                                                {/if}
                                                                <button class="pa-file-btn del" on:click={() => deletePrinterFile(printer, file.name)} title="Delete from {printer.name}" aria-label="Delete {file.name}">🗑️</button>
                                                            </li>
                                                        {/each}
                                                    </ul>

                                                    <button class="pa-clear-all" on:click={() => clearPrinterFiles(printer)} disabled={clearing === printer.id}>
                                                        {clearing === printer.id ? 'Clearing...' : `🧹 Clear all ${files.length} file(s)`}
                                                    </button>
                                                {/if}
                                            </div>
                                        {/if}
                                    {#if printer.online}

                                        {#if canStart(printer)}
                                            <button class="pa-start-toggle" aria-expanded={startFor === printer.id} on:click={() => toggleStart(printer)}>
                                                {startFor === printer.id ? '✕ Close' : '▶ Start a print'}
                                            </button>
                                        {/if}

                                        {#if startFor === printer.id}
                                            <div class="pa-start" id="start-{printer.id}">
                                                <label class="pa-start-row">
                                                    <span>File</span>
                                                    <select bind:value={startFile}>
                                                        <option value="">{files ? (files.length ? 'Choose a file...' : 'No files on this printer') : 'Loading files...'}</option>
                                                        {#each files || [] as file}
                                                            <option value={file.name}>{file.name}</option>
                                                        {/each}
                                                    </select>
                                                </label>

                                                <label class="pa-start-row">
                                                    <span>Plate fitted</span>
                                                    <select bind:value={startBed}>
                                                        {#each BED_TYPES as bed}<option value={bed.value}>{bed.label}</option>{/each}
                                                    </select>
                                                </label>

                                                <label class="pa-start-row">
                                                    <span>Filament</span>
                                                    <select bind:value={startSpool}>
                                                        {#each amsChoices(printer) as choice}
                                                            <option value={String(choice.index)}>
                                                                AMS {choice.unit} slot {choice.slot} — {choice.label}{choice.remain >= 0 ? ` (${choice.remain}%)` : ''}
                                                            </option>
                                                        {/each}
                                                        <option value="external">External spool</option>
                                                    </select>
                                                </label>

                                                {#if startSpool !== 'external'}
                                                    <div class="pa-swatches">
                                                        {#each amsChoices(printer) as choice}
                                                            <button
                                                                class="pa-swatch"
                                                                class:picked={startSpool === String(choice.index)}
                                                                style="background: {choice.color || 'var(--faint)'}"
                                                                title="AMS {choice.unit} slot {choice.slot} — {choice.label}"
                                                                aria-label="AMS {choice.unit} slot {choice.slot}: {choice.label}"
                                                                on:click={() => (startSpool = String(choice.index))}
                                                            >{choice.slot}</button>
                                                        {/each}
                                                    </div>
                                                {/if}

                                                <label class="pa-start-row">
                                                    <span>Plate number</span>
                                                    <input type="number" min="1" max="64" bind:value={startPlate} />
                                                </label>

                                                <div class="pa-start-opts">
                                                    <label><input type="checkbox" bind:checked={startLevel} /> Bed levelling</label>
                                                    <label><input type="checkbox" bind:checked={startFlow} /> Flow calibration</label>
                                                    <label><input type="checkbox" bind:checked={startTimelapse} /> Timelapse</label>
                                                </div>

                                                <p class="pa-start-warn">Check the camera above: the plate must be clear and the plate type must match what you chose.</p>

                                                <button class="pa-start-go" on:click={() => startPrint(printer)} disabled={starting === printer.id || !startFile}>
                                                    {starting === printer.id ? 'Starting...' : '▶ Start print'}
                                                </button>
                                            </div>
                                        {/if}

                                        {#if printer.last_action_by}
                                            <p class="pa-last">Last stopped by {printer.last_action_by}</p>
                                        {/if}
                                    {/if}
                                </div>
                            {/each}
                        </div>
                    {/if}

                    <div class="joblog-head">
                        <h3>📝 Print log</h3>
                        <div class="joblog-tools">
                            <select bind:value={jobPrinter} aria-label="Show prints from">
                                <option value="">All printers</option>
                                {#each jobPrinters as name}<option value={name}>{name}</option>{/each}
                            </select>
                            <!-- A plain link would go without the session token and get a 401 -->
                            <button type="button" class="calendar-link link-btn" on:click={() => exportCSV('/api/admin/export-print-jobs-csv', 'print_jobs.csv')}>Export CSV</button>
                        </div>
                    </div>
                    <p class="subtitle-text">Recorded automatically from the printers - nobody fills in a form. Whose print it is comes from the file name.</p>

                    {#if shownJobs.length === 0}
                        <p class="no-items">No prints recorded yet.</p>
                    {:else}
                        <div class="joblog">
                            {#each shownJobs.slice(0, showAllJobs ? shownJobs.length : 40) as job}
                                <div class="joblog-row {job.result}">
                                    <span class="joblog-when">
                                        {new Date(job.started_at).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}
                                        {new Date(job.started_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                    </span>
                                    <span class="joblog-printer">{job.printer_name}</span>
                                    <span class="joblog-file" title={job.file_name}>{job.file_name || '(unnamed)'}</span>
                                    <span class="joblog-dur">{jobDuration(job)}</span>
                                    <span class="joblog-result {job.result}">
                                        {jobResult(job)}
                                    </span>
                                </div>
                            {/each}
                        </div>
                        {#if shownJobs.length > 40}
                            <button class="link-btn more-btn" on:click={() => (showAllJobs = !showAllJobs)}>
                                {showAllJobs ? 'Show the latest 40' : `Show all ${shownJobs.length} prints`}
                            </button>
                        {/if}
                    {/if}
                </div>
            {/if}

            <!-- Bookings View -->
            {#if currentView === 'bookings'}
                <div class="loans-content">
                    <h2>📅 Lab Bookings</h2>
                    <p class="subtitle-text">
                        The Motion Capture Lab and the Mech Lab robots.
                        <a class="calendar-link" href={bookingLab === 'mocap' ? '/mocap' : '/mechlab'} target="_blank" rel="noopener">Open calendar view →</a>
                    </p>
                    <div class="status-filters" role="tablist">
                        <button class="filter-btn" role="tab" aria-selected={bookingLab === 'mocap'} class:active={bookingLab === 'mocap'} on:click={() => (bookingLab = 'mocap')}>🎥 Mocap Lab</button>
                        <button class="filter-btn" role="tab" aria-selected={bookingLab === 'mech'} class:active={bookingLab === 'mech'} on:click={() => (bookingLab = 'mech')}>🤖 Mech Lab robots</button>
                    </div>
                    <div class="embedded-calendar">
                        {#key `${bookingLab}-${calendarVersion}`}
                            <BookingPage lab={bookingLab} title={bookingLab === 'mocap' ? 'Motion Capture Lab' : 'Mech Lab robots'} embedded />
                        {/key}
                    </div>

                    <div class="management-section">
                        <div class="section-header">
                            <h3>🤖 Robots and labs</h3>
                            <button class="toggle-btn" on:click={() => editResource(null)}>+ Add robot</button>
                        </div>
                        <p class="section-note">Each robot has its own calendar. Research threads get access from a lab admin on the Threads page; "needs approval" also sends every booking to a lab admin.</p>

                        {#if editingResource && !editingResource.ID}
                            <div class="form-card inline-form">{@render resourceForm()}</div>
                        {/if}

                        {#if resources.length === 0}
                            <p class="no-data">No robots yet.</p>
                        {:else}
                            <div class="admin-list">
                                {#each resources as entry (entry.resource.ID)}
                                    {@const r = entry.resource}
                                    <div class="admin-card" class:inactive={!r.active}>
                                        <div class="admin-card-info">
                                            <h4>{r.lab === 'mocap' ? '🎥' : '🤖'} {r.name}</h4>
                                            <p class="admin-username">#{r.slug}</p>
                                            <p class="admin-role">{r.lab === 'mocap' ? 'Mocap Lab' : r.lab === 'mech' ? 'Mech Lab' : r.lab}{r.active ? '' : ' · not bookable'}</p>
                                            <div class="tag-row">
                                                {#if r.requires_thread}<span class="tag">threads only</span>{/if}
                                                {#if r.requires_approval}<span class="tag warn">needs approval</span>{/if}
                                                {#if r.max_hours}<span class="tag">≤ {r.max_hours} h</span>{/if}
                                                {#if r.max_future_bookings}<span class="tag">≤ {r.max_future_bookings} upcoming</span>{/if}
                                            </div>
                                        </div>
                                        <div class="admin-card-actions">
                                            <button class="toggle-btn" on:click={() => editResource(entry)}>✏️ Edit</button>
                                        </div>
                                    </div>
                                    {#if editingResource && editingResource.ID === r.ID}
                                        <div class="form-card inline-form full-row">{@render resourceForm()}</div>
                                    {/if}
                                {/each}
                            </div>
                        {/if}
                    </div>

                    <div class="list-head">
                        <h3 class="cal-list-title">All bookings</h3>
                        <div class="status-filters compact">
                            <button class="filter-btn" class:active={bookingFilter === 'upcoming'} on:click={() => (bookingFilter = 'upcoming')}>Upcoming</button>
                            <button class="filter-btn" class:active={bookingFilter === 'pending'} on:click={() => (bookingFilter = 'pending')}>Pending</button>
                            <button class="filter-btn" class:active={bookingFilter === 'no-show'} on:click={() => (bookingFilter = 'no-show')}>No-shows</button>
                            <button class="filter-btn" class:active={bookingFilter === 'all'} on:click={() => (bookingFilter = 'all')}>All (from a week ago)</button>
                        </div>
                    </div>

                    {#if loading}
                        <p>Loading bookings...</p>
                    {:else if shownBookings.length === 0}
                        <p class="no-items">No bookings{bookingFilter === 'all' ? '' : ' for this filter'}.</p>
                    {:else}
                        <div class="bookings-list">
                            {#each shownBookings as booking (booking.ID)}
                                <div class="booking-row" class:past={new Date(booking.end_time) < new Date()} class:pending={booking.status === 'pending'} style="--res: {hueFor(booking.resource_id)}">
                                    <div class="booking-when">
                                        <span class="booking-date">{new Date(booking.start_time).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}</span>
                                        <span class="booking-time">
                                            {new Date(booking.start_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                            -
                                            {new Date(booking.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                        </span>
                                    </div>
                                    <div class="booking-info">
                                        <p class="booking-purpose">{booking.purpose}</p>
                                        <p class="booking-by">
                                            <span class="booking-res">{resourceName[booking.resource_id] || 'Unknown resource'}</span>
                                            {#if booking.thread_name} · 🧵 {booking.thread_name}{/if}
                                        </p>
                                        <p class="booking-by">
                                            {booking.booked_by} ·
                                            {#if booking.phone}<button class="clickable-phone" on:click={() => copyToClipboard(booking.phone)} title="Click to copy phone number">{booking.phone}</button>{:else}—{/if}
                                        </p>
                                        <div class="tag-row">
                                            {#if booking.status === 'pending'}<span class="tag warn">awaiting approval</span>{/if}
                                            {#if booking.reviewed_by}<span class="tag">reviewed by {booking.reviewed_by}</span>{/if}
                                            {#if booking.no_show}<span class="tag danger">no-show{#if booking.no_show_reported_by} · reported by {booking.no_show_reported_by}{/if}</span>{/if}
                                        </div>
                                    </div>
                                    <div class="booking-actions">
                                        {#if booking.no_show}
                                            <button class="toggle-btn" on:click={() => clearNoShow(booking)}>Clear no-show</button>
                                        {/if}
                                        <button class="delete-admin-btn" on:click={() => deleteBooking(booking)}>🗑️ Delete</button>
                                    </div>
                                </div>
                            {/each}
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Approvals View (loan and booking approvers) -->
            {#if currentView === 'approvals'}
                <div class="loans-content">
                    <h2>✅ Approvals</h2>
                    <p class="subtitle-text">Requests waiting for a decision. Someone else has to approve your own requests.</p>

                    {#if can(adminInfo, 'approve_loans')}
                        <h3 class="approval-title">📦 Loan requests {#if pendingLoans.length}<span class="badge">{pendingLoans.length}</span>{/if}</h3>
                        {#if pendingLoans.length === 0}
                            <p class="no-items">No loan requests waiting.</p>
                        {:else}
                            <div class="loans-grid">
                                {#each pendingLoans as loan (loan.ID)}
                                    <div class="loan-card pending">
                                        <div class="loan-header">
                                            <div class="loan-title-section">
                                                <h3>{loan.item_name}</h3>
                                                {#if loan.photo_filename}
                                                    <div class="item-image">
                                                        <img src="/api/photos/{loan.photo_filename}" alt={loan.item_name} on:error={(e) => (e.target.style.display = 'none')} />
                                                    </div>
                                                {/if}
                                            </div>
                                            <div class="status-badges"><span class="status-badge pending">Pending</span></div>
                                        </div>
                                        <div class="loan-details">
                                            <p><strong>Borrower:</strong> {loan.borrower_name} <span class="acct-tag" class:guest={!loan.user_id} title={loan.user_id ? "Borrowed while signed in" : "Borrowed without an account: reach them on this phone number"}>{loan.user_id ? "👤 account" : "📵 no account"}</span></p>
                                            <p><strong>Lab:</strong> {loan.lab_location}</p>
                                            <p><strong>Quantity:</strong> {loan.quantity_borrowed}</p>
                                            <p><strong>Expected Return:</strong> {formatExpectedReturn(loan.expected_return_date)}</p>
                                            <p><strong>Requested:</strong> {formatDate(loan.CreatedAt)}</p>
                                        </div>
                                        <div class="loan-actions">
                                            {#if loan.user_id === adminInfo.id}
                                                <p class="own-note">This is your own request - another approver has to decide it.</p>
                                            {:else}
                                                <button class="approve-btn" on:click={() => decideLoan(loan, 'approve')} disabled={deciding === 'loan' + loan.ID}>✅ Approve</button>
                                            {/if}
                                            <button class="deny-btn" on:click={() => decideLoan(loan, 'reject')} disabled={deciding === 'loan' + loan.ID}>✕ Decline</button>
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        {/if}
                    {/if}

                    {#if can(adminInfo, 'approve_mech_bookings')}
                        <h3 class="approval-title">🤖 Booking requests {#if pendingBookings.length}<span class="badge">{pendingBookings.length}</span>{/if}</h3>
                        {#if pendingBookings.length === 0}
                            <p class="no-items">No booking requests waiting.</p>
                        {:else}
                            <div class="bookings-list">
                                {#each pendingBookings as booking (booking.ID)}
                                    <div class="booking-row pending" style="--res: {hueFor(booking.resource_id)}">
                                        <div class="booking-when">
                                            <span class="booking-date">{new Date(booking.start_time).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}</span>
                                            <span class="booking-time">
                                                {new Date(booking.start_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                                -
                                                {new Date(booking.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                            </span>
                                        </div>
                                        <div class="booking-info">
                                            <p class="booking-purpose">{booking.purpose}</p>
                                            <p class="booking-by">
                                                <span class="booking-res">{resourceName[booking.resource_id] || 'Unknown resource'}</span>
                                                {#if booking.thread_name} · 🧵 {booking.thread_name}{/if}
                                            </p>
                                            <p class="booking-by">Requested by {booking.booked_by}</p>
                                        </div>
                                        <div class="booking-actions">
                                            {#if booking.user_id === adminInfo.id}
                                                <span class="own-note">Your own request</span>
                                            {:else}
                                                <button class="approve-btn" on:click={() => decideBooking(booking, 'approve')} disabled={deciding === 'booking' + booking.ID}>✅ Approve</button>
                                            {/if}
                                            <button class="deny-btn" on:click={() => decideBooking(booking, 'reject')} disabled={deciding === 'booking' + booking.ID}>✕ Decline</button>
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        {/if}
                    {/if}
                </div>
            {/if}

            <!-- Item History View -->
            {#if currentView === 'history'}
                <div class="loans-content">
                    <h2>📚 Complete Item History</h2>
                    <p class="subtitle-text">Chronological history of all items - borrowed, returned and missing</p>

                    <div class="history-search-container">
                        <label for="history-search">Search history by name, item, phone, lab, purpose, status, or admin:</label>
                        <div class="search-input-group">
                            <input type="text" id="history-search" bind:value={historySearchQuery} placeholder="Search through all history records..." autocomplete="off" class="history-search-input" />
                            {#if historySearchQuery && historySearchQuery.trim()}
                                <button class="clear-search-btn" on:click={clearHistorySearch} title="Clear search" aria-label="Clear search" type="button">✕</button>
                            {/if}
                        </div>
                        <div class="status-filters compact history-filters">
                            <button class="filter-btn" class:active={historyStatus === 'all'} on:click={() => (historyStatus = 'all')}>All</button>
                            <button class="filter-btn" class:active={historyStatus === 'active'} on:click={() => (historyStatus = 'active')}>Borrowed</button>
                            <button class="filter-btn" class:active={historyStatus === 'overdue'} on:click={() => (historyStatus = 'overdue')}>Overdue</button>
                            <button class="filter-btn" class:active={historyStatus === 'returned'} on:click={() => (historyStatus = 'returned')}>Returned</button>
                            <button class="filter-btn" class:active={historyStatus === 'not_found'} on:click={() => (historyStatus = 'not_found')}>Missing</button>
                        </div>
                        {#if historySearchQuery.trim() || historyStatus !== 'all'}
                            <p class="search-results-info">Showing {filteredHistoryItems.length} of {historyItems.length} history records</p>
                        {/if}
                    </div>

                    {#if loading}
                        <p>Loading item history...</p>
                    {:else if historyItems.length === 0}
                        <p class="no-items">No history found.</p>
                    {:else if filteredHistoryItems.length === 0}
                        <div class="no-search-results">
                            <p>No history records match{historySearchQuery.trim() ? ` your search for "${historySearchQuery}"` : ' this filter'}</p>
                            <button class="clear-search-btn-large" on:click={(e) => { historyStatus = 'all'; clearHistorySearch(e); }}>Clear Search</button>
                        </div>
                    {:else}
                        <div class="history-list">
                            {#each filteredHistoryItems as item}
                                {@const late = isOverdue(item.expected_return_date)}
                                <div class="history-item {item.status}" data-status={item.status}>
                                    <div class="history-timeline">
                                        <div class="timeline-dot {item.status}"></div>
                                        <div class="timeline-line"></div>
                                    </div>
                                    <div class="history-content">
                                        <div class="history-header">
                                            <div class="history-title-section">
                                                <h3>{item.item_name}</h3>
                                                {#if item.photo_filename}
                                                    <div class="item-image">
                                                        <img src="/api/photos/{item.photo_filename}" alt={item.item_name} on:error={(e) => (e.target.style.display = 'none')} />
                                                    </div>
                                                {:else}
                                                    <div class="item-image placeholder"><span>📷</span></div>
                                                {/if}
                                            </div>
                                            <div class="history-status-badges">
                                                <span class="status-badge {item.status === 'active' ? 'approved' : item.status === 'not_found' ? 'not-found' : item.status}">
                                                    {#if item.status === 'returned'}✅ Returned
                                                    {:else if item.status === 'not_found'}❌ Missing
                                                    {:else if item.status === 'active'}📋 Borrowed
                                                    {:else}{item.status.charAt(0).toUpperCase() + item.status.slice(1)}{/if}
                                                </span>
                                                {#if item.status === 'active' && late}
                                                    <span class="overdue-badge">OVERDUE</span>
                                                {:else if item.status === 'returned' && late}
                                                    <span class="status-badge return-pending">Late</span>
                                                {/if}
                                            </div>
                                            <div class="history-date">
                                                <span class="date-label">
                                                    {#if item.status === 'returned'}Returned:{:else if item.status === 'not_found'}Marked Missing:{:else}Borrowed:{/if}
                                                </span>
                                                <span class="date-value">
                                                    {#if item.status === 'returned' && item.returned_at}{formatDate(item.returned_at)}
                                                    {:else if item.status === 'not_found' && item.approved_at}{formatDate(item.approved_at)}
                                                    {:else}{formatDate(item.CreatedAt)}{/if}
                                                </span>
                                            </div>
                                        </div>
                                        <div class="history-details">
                                            <div class="detail-row">
                                                <div class="detail-group">
                                                    <p><strong>Borrower:</strong> {item.borrower_name} <span class="acct-tag" class:guest={!item.user_id} title={item.user_id ? "Borrowed while signed in" : "Borrowed without an account: reach them on this phone number"}>{item.user_id ? "👤 account" : "📵 no account"}</span></p>
                                                    <p><strong>Phone:</strong> {#if item.borrower_phone}<button class="clickable-phone" on:click={() => copyToClipboard(item.borrower_phone)} title="Click to copy phone number">{item.borrower_phone}</button>{:else}—{/if}</p>
                                                    <p><strong>Lab:</strong> {item.lab_location}</p>
                                                </div>
                                                <div class="detail-group">
                                                    <p><strong>Quantity:</strong> {item.quantity_borrowed}</p>
                                                    <p><strong>Purpose:</strong> {item.purpose || '—'}</p>
                                                    <p><strong>Last handled by:</strong> {item.approved_by || '—'}</p>
                                                </div>
                                                <div class="detail-group">
                                                    <p><strong>Requested:</strong> {formatDate(item.CreatedAt)}</p>
                                                    {#if item.expected_return_date}
                                                        <p><strong>Expected Return:</strong> <span class:overdue-text={item.status === 'active' && late}>{formatExpectedReturn(item.expected_return_date)}</span></p>
                                                    {/if}
                                                    {#if item.status === 'returned' && item.returned_at}
                                                        <p><strong>Return Date:</strong> {formatDate(item.returned_at)}</p>
                                                    {/if}
                                                    {#if item.status === 'returned' && late}
                                                        <p><strong>Return Status:</strong> <span class="overdue-mark">⚠️ Returned Overdue</span></p>
                                                    {/if}
                                                </div>
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            {/each}
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Lab View -->
            {#if currentView === 'lab-view'}
                <div class="loans-content">
                    <h2>🏭 {selectedLab} - Borrowed Items</h2>
                    <p class="subtitle-text">Items returned more than 2 weeks ago are automatically archived - see them under "Archived", or everything in the "📚 Item History" tab.</p>

                    <div class="status-filters">
                        <button class="filter-btn" class:active={selectedFilter === 'all'} on:click={() => filterLabLoans('all')}>All</button>
                        <button class="filter-btn" class:active={selectedFilter === 'borrowed'} on:click={() => filterLabLoans('borrowed')}>Borrowed</button>
                        <button class="filter-btn" class:active={selectedFilter === 'returned'} on:click={() => filterLabLoans('returned')}>Returned</button>
                        <button class="filter-btn" class:active={selectedFilter === 'not_found'} on:click={() => filterLabLoans('not_found')}>Missing</button>
                        <button class="filter-btn" class:active={selectedFilter === 'archived'} on:click={() => filterLabLoans('archived')}>🗄️ Archived</button>
                    </div>

                    {#if loading}
                        <p>Loading lab items...</p>
                    {:else if labLoans.length === 0}
                        <p class="no-items">No items found for the selected filter.</p>
                    {:else}
                        <div class="loans-grid">
                            {#each labLoans as loan}
                                {@const overdue = isOverdue(loan.expected_return_date) && loan.status === 'active'}
                                <div class="loan-card" class:overdue class:archived={selectedFilter === 'archived'} class:lost-missing={loan.status === 'not_found'}>
                                    <div class="loan-header">
                                        <div class="loan-title-section">
                                            <h3>{loan.item_name}</h3>
                                            {#if loan.photo_filename}
                                                <div class="item-image">
                                                    <img src="/api/photos/{loan.photo_filename}" alt={loan.item_name} on:error={(e) => (e.target.style.display = 'none')} />
                                                </div>
                                            {:else}
                                                <div class="item-image placeholder"><span>📷</span></div>
                                            {/if}
                                        </div>
                                        <div class="status-badges">
                                            {#if selectedFilter === 'archived'}
                                                <span class="status-badge archived">Archived</span>
                                            {:else if loan.status === 'not_found'}
                                                <span class="status-badge not-found">Missing</span>
                                            {:else if loan.status === 'returned'}
                                                <span class="status-badge returned">Returned</span>
                                            {:else}
                                                <span class="status-badge approved">Borrowed</span>
                                            {/if}
                                            {#if overdue}<span class="overdue-badge">OVERDUE</span>{/if}
                                        </div>
                                    </div>
                                    <div class="loan-details">
                                        <p><strong>Borrower:</strong> {loan.borrower_name} <span class="acct-tag" class:guest={!loan.user_id} title={loan.user_id ? "Borrowed while signed in" : "Borrowed without an account: reach them on this phone number"}>{loan.user_id ? "👤 account" : "📵 no account"}</span></p>
                                        <p><strong>Phone:</strong> {#if loan.borrower_phone}<button class="clickable-phone" on:click={() => copyToClipboard(loan.borrower_phone)} title="Click to copy phone number">{loan.borrower_phone}</button>{:else}—{/if}</p>
                                        <p><strong>Quantity:</strong> {loan.quantity_borrowed}</p>
                                        <p><strong>Purpose:</strong> {loan.purpose || '—'}</p>
                                        <p><strong>Expected Return:</strong> <span class:overdue-text={overdue}>{formatExpectedReturn(loan.expected_return_date)}</span></p>
                                        <p><strong>Borrowed:</strong> {formatDate(loan.CreatedAt)}</p>
                                        {#if loan.approved_by}<p><strong>Last handled by:</strong> {loan.approved_by}</p>{/if}
                                    </div>
                                    {#if loan.status === 'active'}
                                        <div class="loan-actions">
                                            <div class="extend-controls">
                                                <span class="extend-label">Extend by:</span>
                                                <div class="extend-inputs">
                                                    <input type="number" id="extend-days-{loan.ID}" aria-label="Extend by days" min="0" max="365" placeholder="Days" value="0" />
                                                    <span>days</span>
                                                    <input type="number" id="extend-hours-{loan.ID}" aria-label="Extend by hours" min="0" max="23" placeholder="Hours" value="0" />
                                                    <span>hours</span>
                                                </div>
                                            </div>
                                            <button
                                                class="extend-btn"
                                                on:click={() => {
                                                    const days = document.getElementById(`extend-days-${loan.ID}`).value;
                                                    const hours = document.getElementById(`extend-hours-${loan.ID}`).value;
                                                    extendLoan(loan.ID, days, hours);
                                                }}
                                                disabled={loading}
                                            >📅 Extend</button>
                                            <button class="not-found-btn" on:click={() => markAsMissing(loan.ID, loan.item_name)} disabled={loading}>❓ Can't Find This Item</button>
                                        </div>
                                    {/if}
                                    {#if loan.status === 'returned'}
                                        <div class="return-section">
                                            <p class="return-status">✅ Returned{#if loan.returned_at} on {formatDate(loan.returned_at)}{/if}</p>
                                            {#if isOverdue(loan.expected_return_date)}
                                                <p class="overdue-returned"><strong>Status:</strong> <span class="overdue-mark">⚠️ Returned Overdue</span></p>
                                            {/if}
                                        </div>
                                    {:else if loan.status === 'not_found'}
                                        <div class="found-section">
                                            <p class="found-notice">🔍 Item marked as missing</p>
                                            <div class="found-actions">
                                                <button class="found-btn" on:click={() => markAsFound(loan.ID)} disabled={loading}>✅ Mark as Found</button>
                                            </div>
                                        </div>
                                    {/if}
                                </div>
                            {/each}
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Item Catalogue View -->
            {#if currentView === 'items'}
                <div class="loans-content">
                    <h2>📦 Item Catalogue</h2>
                    <p class="subtitle-text">People add items as they borrow. Here you tidy up: merge duplicates, mark valuable items as tracked (their custodian confirms them monthly), set rules.</p>

                    <div class="history-search-container">
                        <label for="item-search">Search items by name:</label>
                        <div class="search-input-group">
                            <input type="text" id="item-search" bind:value={itemSearch} placeholder="Search the catalogue..." autocomplete="off" class="history-search-input" />
                            {#if itemSearch.trim()}
                                <button class="clear-search-btn" on:click={() => (itemSearch = '')} title="Clear search" aria-label="Clear search" type="button">✕</button>
                            {/if}
                            <button class="refresh-btn" on:click={loadCatalogue}>🔄 Refresh</button>
                        </div>
                        <div class="status-filters compact history-filters">
                            <button class="filter-btn" class:active={itemFilter === 'all'} on:click={() => (itemFilter = 'all')}>All</button>
                            <button class="filter-btn" class:active={itemFilter === 'out'} on:click={() => (itemFilter = 'out')}>Out now</button>
                            <button class="filter-btn" class:active={itemFilter === 'issue'} on:click={() => (itemFilter = 'issue')}>⚠️ With issues</button>
                            <button class="filter-btn" class:active={itemFilter === 'tracked'} on:click={() => (itemFilter = 'tracked')}>Tracked</button>
                            <button class="filter-btn" class:active={itemFilter === 'approval'} on:click={() => (itemFilter = 'approval')}>Needs approval</button>
                            <button class="filter-btn" class:active={itemFilter === 'lab_only'} on:click={() => (itemFilter = 'lab_only')}>Lab only</button>
                        </div>
                        <p class="search-results-info">Showing {shownItems.length} of {catalogue.length} items</p>
                    </div>

                    {#if shownItems.length === 0}
                        <p class="no-items">No items{itemSearch.trim() ? ` match "${itemSearch}"` : ' for this filter'}.</p>
                    {:else}
                        <div class="loans-grid items-grid">
                            {#each shownItems as item (item.ID)}
                                {@const out = itemsOut(item)}
                                <div class="loan-card item-card" class:has-issue={item.issue} class:tracked={item.tracked}>
                                    <div class="loan-header">
                                        <div class="loan-title-section">
                                            <h3>{item.name}</h3>
                                        </div>
                                        <div class="status-badges">
                                            {#if out > 0}<span class="status-badge approved">{out} out</span>{/if}
                                            {#if item.tracked}<span class="status-badge returned">Tracked</span>{/if}
                                            {#if item.needs_approval}<span class="status-badge pending">Needs approval</span>{/if}
                                            {#if item.lab_only}<span class="status-badge archived">Lab only</span>{/if}
                                        </div>
                                    </div>

                                    {#if editingItem && editingItem.ID === item.ID}
                                        <form class="form-card inline-form" on:submit|preventDefault={saveItem}>
                                            <div class="form-group">
                                                <label for="item-name-{item.ID}">Name</label>
                                                <input id="item-name-{item.ID}" type="text" bind:value={editingItem.name} required />
                                            </div>
                                            <div class="form-group">
                                                <label for="item-lab-{item.ID}">Lab</label>
                                                <select id="item-lab-{item.ID}" bind:value={editingItem.home_lab}>
                                                    {#each labs as lab}<option value={lab}>{lab}</option>{/each}
                                                    {#if editingItem.home_lab && !labs.includes(editingItem.home_lab)}
                                                        <option value={editingItem.home_lab}>{editingItem.home_lab}</option>
                                                    {/if}
                                                </select>
                                            </div>
                                            <div class="form-group">
                                                <label for="item-qty-{item.ID}">Total quantity (0 = unknown)</label>
                                                <input id="item-qty-{item.ID}" type="number" min="0" bind:value={editingItem.total_quantity} />
                                            </div>
                                            <div class="form-group">
                                                <label for="item-notes-{item.ID}">Notes</label>
                                                <input id="item-notes-{item.ID}" type="text" maxlength="500" bind:value={editingItem.notes} />
                                            </div>
                                            <div class="form-group">
                                                <label class="checkbox-label"><input type="checkbox" bind:checked={editingItem.tracked} /> Tracked (custodian confirms it monthly)</label>
                                                <label class="checkbox-label"><input type="checkbox" bind:checked={editingItem.lab_only} /> Lab only (can't leave the lab)</label>
                                                <label class="checkbox-label"><input type="checkbox" bind:checked={editingItem.needs_approval} /> Borrowing needs approval</label>
                                            </div>
                                            {#if editingItem.tracked}
                                                <div class="form-group">
                                                    <label for="item-cust-{item.ID}">Custodian</label>
                                                    <select id="item-cust-{item.ID}" bind:value={editingItem.custodian_id}>
                                                        <option value={null}>None</option>
                                                        {#each people as person}<option value={person.id}>{person.name}</option>{/each}
                                                    </select>
                                                </div>
                                            {/if}
                                            <div class="form-actions">
                                                <button type="submit" class="submit-btn">Save</button>
                                                <button type="button" class="cancel-btn" on:click={() => (editingItem = null)}>Cancel</button>
                                            </div>
                                        </form>
                                    {:else}
                                        <div class="loan-details">
                                            <p><strong>Lab:</strong> {item.home_lab || '—'}</p>
                                            <p><strong>Out now:</strong> {out}{#if item.total_quantity} of {item.total_quantity}{/if}</p>
                                            {#if item.tracked}
                                                <p><strong>Custodian:</strong> {personName(item.custodian_id)}</p>
                                                {#if item.last_verified_at}<p><strong>Last confirmed:</strong> {formatDate(item.last_verified_at)}</p>{/if}
                                            {/if}
                                            {#if item.notes}<p><strong>Notes:</strong> {item.notes}</p>{/if}
                                        </div>
                                        {#if item.issue}
                                            <div class="return-section issue-section">
                                                <p class="return-notice">⚠️ {item.issue}</p>
                                                {#if item.issue_reported_by}<p class="issue-by">Reported by {item.issue_reported_by}</p>{/if}
                                                <button class="found-btn" on:click={() => clearIssue(item)}>✅ Clear issue</button>
                                            </div>
                                        {/if}
                                        <div class="loan-actions item-actions">
                                            <button class="extend-btn" on:click={() => (editingItem = { ...item, notes: item.notes || '', custodian_id: item.custodian_id ?? null })}>✏️ Edit</button>
                                            <div class="merge-group">
                                                <select bind:value={mergeInto[item.ID]} aria-label="Merge {item.name} into">
                                                    <option value={undefined}>Merge into…</option>
                                                    {#each catalogue as other (other.ID)}
                                                        {#if other.ID !== item.ID}<option value={other.ID}>{other.name}</option>{/if}
                                                    {/each}
                                                </select>
                                                <button class="not-found-btn" disabled={!mergeInto[item.ID]} on:click={() => mergeItem(item)}>🔀 Merge</button>
                                            </div>
                                            <button class="deny-btn" on:click={() => deleteItem(item)}>🗑️ Delete</button>
                                        </div>
                                    {/if}
                                </div>
                            {/each}
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Change Password View -->
            {#if currentView === 'change-password'}
                <div class="form-container">
                    <h2>🔑 Change Password</h2>
                    <div class="form-card">
                        <p class="form-note">Changing it signs you out everywhere else; this browser stays signed in.</p>
                        <form on:submit|preventDefault={changePassword}>
                            <div class="form-group">
                                <label for="old_password">Current Password</label>
                                <input type="password" id="old_password" bind:value={changePasswordForm.old_password} required autocomplete="current-password" placeholder="Enter current password" />
                            </div>
                            <div class="form-group">
                                <label for="new_password">New Password</label>
                                <input type="password" id="new_password" bind:value={changePasswordForm.new_password} required minlength="8" maxlength="72" autocomplete="new-password" placeholder="Enter new password (min 8 characters)" />
                            </div>
                            <div class="form-group">
                                <label for="confirm_password">Confirm New Password</label>
                                <input type="password" id="confirm_password" bind:value={changePasswordForm.confirm_password} required minlength="8" maxlength="72" autocomplete="new-password" placeholder="Confirm new password" />
                            </div>
                            <div class="form-actions">
                                <button type="submit" class="submit-btn" disabled={savingPassword}>{savingPassword ? 'Changing...' : 'Change Password'}</button>
                                <button type="button" class="cancel-btn" on:click={goToDashboard}>Cancel</button>
                            </div>
                        </form>
                    </div>
                </div>
            {/if}

            <!-- Users & Groups View (server admins) -->
            {#if currentView === 'users' && can(adminInfo, 'manage_users')}
                <div class="admin-management-container">
                    <h2>👥 Users &amp; Groups</h2>

                    <!-- Sign-ups waiting -->
                    <div class="management-section" class:waiting={pendingUsers.length > 0}>
                        <div class="section-header">
                            <h3>⏳ Waiting for approval {#if pendingUsers.length}<span class="badge">{pendingUsers.length}</span>{/if}</h3>
                            <button class="refresh-btn" on:click={loadUsers}>🔄 Refresh</button>
                        </div>
                        {#if pendingUsers.length === 0}
                            <p class="no-data">Nobody is waiting. New sign-ups appear here.</p>
                        {:else}
                            <div class="admin-list">
                                {#each pendingUsers as user (user.id)}
                                    <div class="admin-card pending-card">
                                        <div class="admin-card-info">
                                            <h4>{user.name}</h4>
                                            <p class="admin-username">{user.email}</p>
                                            <p class="admin-phone">📞 {user.phone}</p>
                                            <p class="admin-date">Signed up: {new Date(user.created_at).toLocaleDateString()}</p>
                                            <label class="group-pick">
                                                <span>Group</span>
                                                <select value={user.group_id} on:change={(e) => changeGroup(e, user)} aria-label="Group for {user.name}">
                                                    {#each groups as g}<option value={g.id}>{g.name}</option>{/each}
                                                </select>
                                            </label>
                                        </div>
                                        <div class="admin-card-actions">
                                            <button class="approve-btn" on:click={() => updateUser(user, { status: 'active' }, `${user.name} approved`)}>✅ Approve</button>
                                            <button class="deny-btn" on:click={() => confirm(`Reject ${user.name}'s sign-up?`) && updateUser(user, { status: 'disabled' }, `${user.name} rejected`)}>✕ Reject</button>
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        {/if}
                    </div>

                    <!-- Everyone -->
                    <div class="management-section">
                        <div class="section-header">
                            <h3>Current Users <span class="count-note">{activeUserCount} active · {disabledUserCount} disabled</span></h3>
                        </div>
                        <div class="user-tools">
                            <input type="search" class="history-search-input" bind:value={userSearch} placeholder="Search name, email, phone or group" aria-label="Search users" />
                            <div class="status-filters compact">
                                <button class="filter-btn" class:active={userFilter === 'all'} on:click={() => (userFilter = 'all')}>All</button>
                                <button class="filter-btn" class:active={userFilter === 'active'} on:click={() => (userFilter = 'active')}>Active</button>
                                <button class="filter-btn" class:active={userFilter === 'disabled'} on:click={() => (userFilter = 'disabled')}>Disabled</button>
                                <button class="filter-btn" class:active={userFilter === 'extra'} on:click={() => (userFilter = 'extra')}>Extra access</button>
                            </div>
                        </div>

                        {#if shownUsers.length === 0}
                            <p class="no-data">No users match. <button class="link-btn" on:click={() => { userSearch = ''; userFilter = 'all'; }}>Show everyone</button></p>
                        {:else}
                            <div class="admin-list">
                                {#each shownUsers as user (user.id)}
                                    <div class="admin-card" class:inactive={user.status === 'disabled'} class:editing={editingUser && editingUser.id === user.id}>
                                        <div class="admin-card-info">
                                            <h4>
                                                {user.name}
                                                {#if user.id === adminInfo.id}<span class="current-user-badge">You</span>{/if}
                                            </h4>
                                            <p class="admin-username">{user.email}</p>
                                            {#if user.phone}<p class="admin-phone">📞 {user.phone}</p>{/if}
                                            <div class="admin-role">
                                                {#if user.id === adminInfo.id}
                                                    <span class="group-chip">{groupName(user.group_id)}</span>
                                                {:else}
                                                    <label class="group-pick">
                                                        <span>Group</span>
                                                        <select value={user.group_id} on:change={(e) => changeGroup(e, user)} aria-label="Group for {user.name}">
                                                            {#each groups as g}<option value={g.id}>{g.name}</option>{/each}
                                                        </select>
                                                    </label>
                                                {/if}
                                                <span class="status-badge {user.status === 'active' ? 'approved' : user.status === 'pending' ? 'pending' : 'denied'}">{user.status}</span>
                                            </div>
                                            {#if user.overrides && user.overrides.length > 0}
                                                <div class="tag-row">
                                                    {#each user.overrides as o}
                                                        <span class="tag" class:ok={o.allow} class:danger={!o.allow}>{o.allow ? '+' : '−'} {permLabel(o.perm)}</span>
                                                    {/each}
                                                </div>
                                            {/if}
                                            {#if user.created_at}<p class="admin-date">Joined: {new Date(user.created_at).toLocaleDateString()}</p>{/if}
                                        </div>
                                        {#if user.id !== adminInfo.id}
                                            <div class="admin-card-actions">
                                                <button class="toggle-btn" on:click={() => (editingUser && editingUser.id === user.id ? (editingUser = null) : editOverrides(user))}>🔐 Access</button>
                                                <button class="toggle-btn subtle" on:click={() => resetPassword(user)}>🔑 Reset password</button>
                                                {#if user.status === 'active'}
                                                    <button class="delete-admin-btn" on:click={() => confirm(`Disable ${user.name}? They will be logged out.`) && updateUser(user, { status: 'disabled' }, `${user.name} disabled`)}>🚫 Disable</button>
                                                {:else}
                                                    <button class="refresh-btn" on:click={() => updateUser(user, { status: 'active' }, `${user.name} enabled`)}>✅ Enable</button>
                                                {/if}
                                            </div>
                                        {:else}
                                            <div class="admin-card-actions">
                                                <button class="toggle-btn subtle" on:click={showChangePasswordView}>🔑 Change my password</button>
                                            </div>
                                        {/if}
                                    </div>
                                    {#if editingUser && editingUser.id === user.id}
                                        <div class="form-card override-card full-row">
                                            <h4>🔐 Access for {editingUser.name} <span class="count-note">({groupName(editingUser.group_id)})</span></h4>
                                            <p class="form-note">"Group default" follows the group's settings below. Grant or revoke to make an exception for this person.</p>
                                            <div class="override-grid">
                                                {#each draftPerms as perm}
                                                    <label class="override-row">
                                                        <span>{permLabel(perm)}</span>
                                                        <select bind:value={overrideDraft[perm]} class:grant={overrideDraft[perm] === 'grant'} class:revoke={overrideDraft[perm] === 'revoke'}>
                                                            <option value={undefined}>Group default{groups.find((g) => g.id === editingUser.group_id)?.permissions.includes(perm) ? ' (yes)' : ' (no)'}</option>
                                                            <option value="grant">Grant</option>
                                                            <option value="revoke">Revoke</option>
                                                        </select>
                                                    </label>
                                                {/each}
                                            </div>
                                            <div class="form-actions">
                                                <button class="submit-btn" on:click={saveOverrides}>Save</button>
                                                <button class="cancel-btn" on:click={() => (editingUser = null)}>Cancel</button>
                                            </div>
                                        </div>
                                    {/if}
                                {/each}
                            </div>
                        {/if}
                    </div>

                    <!-- What each group may do -->
                    <div class="management-section">
                        <div class="section-header">
                            <h3>🛡️ Group Permissions</h3>
                        </div>
                        <p class="section-note">What each group can do by default. Changes apply immediately to everyone in the group.</p>
                        <div class="table-wrap">
                            <table class="perm-table">
                                <thead>
                                    <tr>
                                        <th scope="col">Permission</th>
                                        {#each groups as g}
                                            <th scope="col" class="center">{g.name}<span class="group-count">{users.filter((u) => u.group_id === g.id).length} people</span></th>
                                        {/each}
                                    </tr>
                                </thead>
                                <tbody>
                                    {#each allPermissions as perm}
                                        <tr>
                                            <th scope="row">{permLabel(perm)}</th>
                                            {#each groups as g}
                                                <td class="center">
                                                    <input type="checkbox" checked={g.permissions.includes(perm)} on:change={(e) => toggleGroupPerm(e, g, perm)} aria-label="{g.name}: {permLabel(perm)}" />
                                                </td>
                                            {/each}
                                        </tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                    </div>

                    <!-- Danger Zone -->
                    <div class="management-section danger-zone">
                        <div class="section-header">
                            <h3>⚠️ Danger Zone</h3>
                        </div>
                        <div class="danger-actions">
                            <div class="danger-item">
                                <div class="danger-info">
                                    <h4>Delete All Items Data</h4>
                                    <p>Permanently delete all inventory items. This action cannot be undone.</p>
                                </div>
                                <button class="danger-btn" on:click={deleteAllItems}>🗑️ Delete All Items</button>
                            </div>
                        </div>
                    </div>
                </div>
            {/if}
        </div>
    {/if}
</div>

{#snippet resourceForm()}
    <form on:submit|preventDefault={saveResource}>
        <h4 class="form-title">{editingResource.ID ? `✏️ Edit ${editingResource.name}` : '➕ New robot'}</h4>
        <div class="form-group">
            <label for="res-name">Name</label>
            <input id="res-name" type="text" bind:value={editingResource.name} required placeholder="UR5 #1" />
        </div>
        {#if !editingResource.ID}
            <div class="form-group">
                <label for="res-slug">Id (can't change later)</label>
                <input id="res-slug" type="text" bind:value={editingResource.slug} required pattern="[a-z0-9][a-z0-9-]*" placeholder="ur5-1" />
            </div>
        {/if}
        <div class="form-group">
            <label for="res-lab">Lab</label>
            <select id="res-lab" bind:value={editingResource.lab}>
                <option value="mech">Mech Lab</option>
                <option value="mocap">Mocap Lab</option>
            </select>
        </div>
        <div class="form-group">
            <label class="checkbox-label"><input type="checkbox" bind:checked={editingResource.requires_thread} /> Only research threads with access can book it</label>
            <label class="checkbox-label"><input type="checkbox" bind:checked={editingResource.requires_approval} /> Every booking needs a lab admin's approval</label>
            <label class="checkbox-label"><input type="checkbox" bind:checked={editingResource.active} /> Bookable</label>
        </div>
        <div class="form-group">
            <label for="res-hours">Longest booking, hours (0 = 12)</label>
            <input id="res-hours" type="number" min="0" max="168" bind:value={editingResource.max_hours} />
        </div>
        <div class="form-group">
            <label for="res-future">Upcoming bookings per person (0 = no limit)</label>
            <input id="res-future" type="number" min="0" bind:value={editingResource.max_future_bookings} />
        </div>
        <div class="form-actions">
            <button type="submit" class="submit-btn">Save</button>
            <button type="button" class="cancel-btn" on:click={() => (editingResource = null)}>Cancel</button>
        </div>
    </form>
{/snippet}

<style>
    /* The original admin dashboard's look, in Rosé Pine. The site shell
       (lib/Shell.svelte) paints the page and draws the header and footer. */

    /* Same column as every other page, so the admin lines up with the header */
    .admin-container {
        max-width: 1200px;
        margin: 0 auto;
        padding: 20px clamp(16px, 3vw, 28px) 48px;
        box-sizing: border-box;
    }

    .admin-container :global(*),
    .admin-container :global(*::before),
    .admin-container :global(*::after) {
        box-sizing: border-box;
    }

    h2 {
        color: var(--ctp-text);
        font-size: clamp(1.3rem, 3vw, 1.6rem);
        font-weight: 650;
        margin: 0 0 8px;
    }

    code {
        font-family: var(--mono);
        font-size: 0.9em;
        background: var(--ctp-surface0);
        padding: 1px 5px;
        border-radius: 4px;
    }

    /* Dashboard header */
    .dashboard-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: clamp(16px, 3vw, 28px);
        padding-bottom: 20px;
        border-bottom: 2px solid var(--ctp-surface0);
        flex-wrap: wrap;
        gap: 15px;
    }

    .dashboard-title {
        display: flex;
        align-items: center;
        gap: clamp(12px, 2.5vw, 18px);
        flex: 1;
        min-width: 0;
    }

    .dashboard-header h1 {
        margin: 0;
        color: var(--ctp-text);
        font-size: clamp(1.4rem, 3.5vw, 2rem);
        font-weight: 600;
        line-height: 1.2;
    }

    .admin-info {
        display: flex;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
        color: var(--ctp-subtext0);
    }

    .admin-info strong {
        color: var(--ctp-text);
    }

    .role-pill {
        padding: 4px 10px;
        border-radius: var(--radius-pill);
        background: var(--ctp-surface0);
        border: 1px solid var(--ctp-surface1);
        color: var(--ctp-mauve);
        font-size: 0.8rem;
        font-weight: 600;
    }

    .logout-btn {
        background: linear-gradient(135deg, var(--ctp-red), var(--ctp-maroon));
        color: var(--accent-ink);
        border: none;
        min-height: 40px;
        padding: 8px clamp(12px, 3vw, 20px);
        border-radius: 8px;
        cursor: pointer;
        font: inherit;
        font-size: clamp(0.85rem, 1.8vw, 1rem);
        font-weight: 600;
        transition: all 0.3s ease;
    }

    .logout-btn:hover {
        filter: brightness(1.08);
        transform: translateY(-1px);
        box-shadow: 0 4px 15px color-mix(in srgb, var(--ctp-red) 30%, transparent);
    }

    /* Navigation tabs: a strip that scrolls sideways on narrow screens */
    .nav-tabs {
        display: flex;
        gap: clamp(4px, 0.8vw, 10px);
        margin-bottom: clamp(18px, 3vw, 32px);
        border-bottom: 2px solid var(--ctp-surface0);
        overflow-x: auto;
        overflow-y: hidden;
        scroll-snap-type: x proximity;
        -webkit-overflow-scrolling: touch;
        scrollbar-width: thin;
        /* The tab strip is the primary navigation - keep it in reach */
        position: sticky;
        top: 0;
        z-index: 5;
        background: linear-gradient(var(--bg) 80%, transparent);
        padding-bottom: 2px;
    }

    .nav-tabs::-webkit-scrollbar {
        height: 4px;
    }

    /* Wide screens: every tab visible at once, on two rows if needed */
    @media (min-width: 900px) {
        .nav-tabs {
            flex-wrap: wrap;
            overflow: visible;
        }
    }

    .tab-btn {
        flex: none;
        background: none;
        border: none;
        min-height: 44px;
        padding: 10px clamp(12px, 2vw, 20px);
        cursor: pointer;
        border-bottom: 3px solid transparent;
        font-size: clamp(0.88rem, 1.7vw, 1rem);
        position: relative;
        color: var(--ctp-subtext0);
        white-space: nowrap;
        font-weight: 550;
        font-family: inherit;
        border-radius: var(--radius-sm) var(--radius-sm) 0 0;
        scroll-snap-align: start;
        transition:
            color var(--fast) var(--ease),
            background-color var(--fast) var(--ease),
            transform var(--fast) var(--ease);
    }

    .tab-btn:hover {
        color: var(--ctp-text);
        background: color-mix(in srgb, var(--ctp-surface0) 60%, transparent);
        transform: translateY(-2px);
    }

    /* The underline grows out from the middle rather than snapping in */
    .tab-btn::after {
        content: '';
        position: absolute;
        left: 12%;
        right: 12%;
        bottom: -2px;
        height: 3px;
        border-radius: var(--radius-pill);
        background: var(--ctp-mauve);
        transform: scaleX(0);
        transition: transform var(--normal) var(--ease);
    }

    .tab-btn:hover::after {
        transform: scaleX(0.5);
    }

    .tab-btn.active::after {
        transform: scaleX(1);
    }

    .tab-btn.active {
        border-bottom-color: var(--ctp-flamingo);
        color: var(--ctp-flamingo);
        font-weight: 600;
    }

    .badge {
        display: inline-block;
        background: var(--ctp-red);
        color: var(--accent-ink);
        border-radius: 12px;
        padding: 2px 8px;
        font-size: 0.72rem;
        margin-left: 6px;
        font-weight: 700;
        vertical-align: middle;
        line-height: 1.4;
    }

    /* Dashboard overview */
    .overview-cards {
        display: grid;
        /* min() keeps a single column from overflowing a narrow phone */
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 260px), 1fr));
        gap: clamp(12px, 2vw, 20px);
        margin-top: 20px;
    }

    .card {
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--ctp-text) 4%, transparent), transparent 60%),
            var(--ctp-mantle);
        padding: clamp(18px, 3.5vw, 30px);
        border-radius: var(--radius-lg);
        box-shadow: var(--shadow-sm);
        text-align: center;
        border: 1px solid var(--ctp-surface0);
        transition:
            transform var(--normal) var(--ease),
            box-shadow var(--normal) var(--ease),
            border-color var(--normal) var(--ease);
    }

    .card:hover {
        transform: translateY(-4px) scale(1.015);
        border-color: color-mix(in srgb, var(--ctp-mauve) 45%, var(--ctp-surface1));
        box-shadow: var(--shadow);
    }

    /* Something is waiting: the card says so before anyone reads the number */
    .card.attention {
        border-color: color-mix(in srgb, var(--ctp-yellow) 60%, var(--ctp-surface1));
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--ctp-yellow) 10%, transparent), transparent 70%),
            var(--ctp-mantle);
    }

    .card.attention .stat-number {
        color: var(--ctp-yellow);
    }

    .card p:not(.stat-number) {
        color: var(--ctp-subtext0);
    }

    .stat-number {
        font-size: clamp(2.5rem, 8vw, 4rem);
        font-weight: 700;
        color: var(--ctp-flamingo);
        margin: 15px 0;
        line-height: 1;
        font-variant-numeric: tabular-nums;
    }

    .card h3 {
        color: var(--ctp-text);
        margin: 0 0 15px;
        font-size: clamp(1.1rem, 2.5vw, 1.3rem);
    }

    .card-btn {
        background: linear-gradient(135deg, var(--ctp-flamingo), var(--ctp-maroon));
        color: var(--accent-ink);
        border: none;
        min-height: 42px;
        padding: 8px clamp(16px, 3vw, 24px);
        border-radius: 8px;
        cursor: pointer;
        font: inherit;
        font-size: clamp(0.88rem, 1.8vw, 1rem);
        font-weight: 600;
        transition: all 0.3s ease;
    }

    .card-btn:hover {
        filter: brightness(1.06);
        transform: translateY(-1px);
        box-shadow: 0 4px 15px color-mix(in srgb, var(--ctp-flamingo) 30%, transparent);
    }

    .export-actions {
        display: flex;
        flex-wrap: wrap;
        gap: 10px;
        margin-top: 30px;
        padding: clamp(18px, 4vw, 25px);
        background: var(--ctp-crust);
        border-radius: 12px;
        border: 1px solid var(--ctp-surface0);
    }

    .export-btn {
        border: none;
        min-height: 44px;
        padding: 10px clamp(16px, 3vw, 24px);
        border-radius: 8px;
        cursor: pointer;
        font: inherit;
        font-size: clamp(0.88rem, 1.8vw, 1rem);
        font-weight: 600;
        transition: all 0.3s ease;
        background: linear-gradient(135deg, var(--ctp-teal), var(--ctp-blue));
        color: var(--rp-base);
    }

    .export-btn:hover {
        filter: brightness(1.06);
        transform: translateY(-1px);
        box-shadow: 0 4px 15px color-mix(in srgb, var(--ctp-teal) 30%, transparent);
    }

    /* Status filters */
    .status-filters {
        display: flex;
        gap: clamp(8px, 2vw, 12px);
        margin-bottom: clamp(15px, 3vw, 25px);
        flex-wrap: wrap;
    }

    .status-filters.compact {
        margin-bottom: 0;
        gap: 8px;
    }

    .filter-btn {
        background: var(--ctp-crust);
        border: 2px solid var(--ctp-surface0);
        color: var(--ctp-subtext0);
        min-height: 40px;
        padding: 6px clamp(12px, 3vw, 18px);
        border-radius: 20px;
        cursor: pointer;
        font: inherit;
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
        transition: all 0.3s ease;
        font-weight: 500;
    }

    .filter-btn:hover {
        background: var(--ctp-surface0);
        border-color: var(--ctp-flamingo);
        color: var(--ctp-text);
    }

    .filter-btn.active {
        background: var(--ctp-flamingo);
        border-color: var(--ctp-flamingo);
        color: var(--accent-ink);
        font-weight: 600;
    }

    /* Loan cards */
    .loans-grid {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(min(100%, 450px), 1fr));
        gap: clamp(15px, 3vw, 25px);
        margin-top: 20px;
    }

    .loan-card {
        background: var(--ctp-crust);
        border-radius: 12px;
        padding: clamp(16px, 4vw, 25px);
        box-shadow: var(--shadow);
        border: 1px solid var(--ctp-surface0);
        border-left: 5px solid var(--ctp-flamingo);
        transition: all 0.3s ease;
        min-width: 0;
    }

    .loan-card:hover {
        transform: translateY(-2px);
        box-shadow: var(--shadow-lg);
        border-color: var(--ctp-flamingo);
    }

    .loan-card.pending {
        border-left-color: var(--warn);
        background: color-mix(in srgb, var(--warn) 6%, var(--ctp-crust));
    }

    .loan-card.overdue {
        border-left-color: var(--danger);
        background: color-mix(in srgb, var(--danger) 5%, var(--ctp-crust));
    }

    .loan-card.lost-missing {
        border-left-color: var(--ctp-peach);
        background: color-mix(in srgb, var(--ctp-peach) 5%, var(--ctp-crust));
    }

    .loan-card.archived {
        border-left-color: var(--ctp-overlay0);
        background: color-mix(in srgb, var(--ctp-overlay0) 5%, var(--ctp-crust));
        opacity: 0.9;
    }

    .loan-header {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        margin-bottom: 15px;
        gap: 15px;
    }

    .loan-title-section {
        display: flex;
        align-items: flex-start;
        gap: 15px;
        flex: 1;
        min-width: 0;
    }

    .loan-title-section h3 {
        margin: 0;
        color: var(--ctp-text);
        font-size: clamp(1.1rem, 2.5vw, 1.3rem);
        flex: 1;
        font-weight: 600;
        overflow-wrap: anywhere;
    }

    .status-badges {
        display: flex;
        flex-direction: column;
        gap: 5px;
        align-items: flex-end;
    }

    .status-badge {
        padding: 4px 10px;
        border-radius: 12px;
        font-size: 0.72rem;
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.5px;
        white-space: nowrap;
        border: 1px solid transparent;
    }

    .status-badge.pending,
    .status-badge.return-pending {
        background: color-mix(in srgb, var(--warn) 18%, transparent);
        color: var(--warn);
        border-color: color-mix(in srgb, var(--warn) 30%, transparent);
    }

    .status-badge.approved {
        background: color-mix(in srgb, var(--ctp-green) 18%, transparent);
        color: var(--ctp-green);
        border-color: color-mix(in srgb, var(--ctp-green) 30%, transparent);
    }

    .status-badge.returned {
        background: color-mix(in srgb, var(--ctp-blue) 18%, transparent);
        color: var(--link);
        border-color: color-mix(in srgb, var(--ctp-blue) 35%, transparent);
    }

    .status-badge.denied,
    .status-badge.archived {
        background: color-mix(in srgb, var(--ctp-overlay0) 18%, transparent);
        color: var(--ctp-subtext0);
        border-color: color-mix(in srgb, var(--ctp-overlay0) 30%, transparent);
    }

    .status-badge.not-found,
    .status-badge.not_found {
        background: color-mix(in srgb, var(--ctp-peach) 18%, transparent);
        color: var(--ctp-peach);
        border-color: color-mix(in srgb, var(--ctp-peach) 30%, transparent);
    }

    .overdue-badge {
        background: var(--danger);
        color: var(--accent-ink);
        padding: 4px 10px;
        border-radius: 12px;
        font-size: 0.72rem;
        font-weight: 700;
        white-space: nowrap;
        animation: pulse 2s infinite;
    }

    @keyframes pulse {
        0% { opacity: 1; box-shadow: 0 0 0 0 color-mix(in srgb, var(--danger) 70%, transparent); }
        50% { opacity: 0.8; box-shadow: 0 0 0 8px transparent; }
        100% { opacity: 1; box-shadow: 0 0 0 0 transparent; }
    }

    .loan-details p {
        margin: clamp(6px, 1.5vw, 10px) 0;
        color: var(--ctp-subtext0);
        font-size: clamp(0.88rem, 1.8vw, 0.95rem);
        overflow-wrap: anywhere;
    }

    .loan-details strong {
        color: var(--ctp-text);
        font-weight: 600;
    }

    /* Whether the borrower had an account: without one, the phone is the
       only way to reach them, so it is flagged */
    .acct-tag {
        display: inline-block;
        margin-left: 6px;
        padding: 1px 8px;
        border-radius: var(--radius-pill);
        font-size: 0.72rem;
        font-weight: 600;
        background: color-mix(in srgb, var(--ctp-blue) 14%, transparent);
        color: var(--link);
        white-space: nowrap;
        vertical-align: middle;
    }

    .acct-tag.guest {
        background: color-mix(in srgb, var(--warn) 16%, transparent);
        color: var(--warn);
    }

    .clickable-phone {
        background: none;
        border: none;
        font: inherit;
        color: var(--link);
        cursor: copy;
        text-decoration: underline;
        text-decoration-style: dotted;
        transition: all 0.2s ease;
        padding: 2px 4px;
        border-radius: 3px;
    }

    .clickable-phone:hover {
        color: var(--ctp-lavender);
        background-color: var(--ctp-surface0);
        text-decoration-style: solid;
    }

    .item-image {
        width: clamp(64px, 15vw, 90px);
        height: clamp(64px, 15vw, 90px);
        border-radius: 8px;
        overflow: hidden;
        border: 2px solid var(--ctp-surface0);
        flex-shrink: 0;
    }

    .item-image img {
        width: 100%;
        height: 100%;
        object-fit: cover;
        display: block;
    }

    .item-image.placeholder {
        display: flex;
        align-items: center;
        justify-content: center;
        background: var(--ctp-mantle);
        color: var(--ctp-subtext0);
        font-size: clamp(1.2rem, 3vw, 1.8rem);
    }

    .loan-actions {
        display: flex;
        gap: clamp(8px, 2vw, 12px);
        margin-top: 15px;
        align-items: center;
        flex-wrap: wrap;
    }

    .approve-btn,
    .deny-btn,
    .extend-btn,
    .not-found-btn {
        border: none;
        min-height: 42px;
        padding: 8px clamp(12px, 3vw, 18px);
        border-radius: 8px;
        cursor: pointer;
        font: inherit;
        font-size: clamp(0.85rem, 1.8vw, 0.92rem);
        font-weight: 600;
        transition: all 0.3s ease;
    }

    .approve-btn {
        background: linear-gradient(135deg, var(--ctp-green), var(--ctp-teal));
        color: var(--rp-base);
    }

    .deny-btn {
        background: linear-gradient(135deg, var(--ctp-red), var(--ctp-maroon));
        color: var(--accent-ink);
    }

    .extend-btn {
        background: linear-gradient(135deg, var(--ctp-flamingo), var(--ctp-maroon));
        color: var(--accent-ink);
    }

    .not-found-btn {
        background: linear-gradient(135deg, var(--ctp-peach), var(--ctp-yellow));
        color: var(--rp-base);
    }

    .approve-btn:hover,
    .deny-btn:hover,
    .extend-btn:hover,
    .not-found-btn:hover {
        filter: brightness(1.07);
        transform: translateY(-1px);
        box-shadow: var(--shadow-sm);
    }

    .approve-btn:disabled,
    .deny-btn:disabled,
    .extend-btn:disabled,
    .not-found-btn:disabled {
        opacity: 0.5;
        cursor: not-allowed;
        transform: none;
        filter: none;
    }

    .extend-controls {
        display: flex;
        flex-direction: column;
        gap: 10px;
        width: 100%;
    }

    .extend-label {
        font-weight: 600;
        color: var(--ctp-text);
    }

    .extend-inputs {
        display: flex;
        align-items: center;
        gap: 8px;
        flex-wrap: wrap;
    }

    .extend-inputs input {
        width: 72px;
        min-height: 40px;
        padding: 6px;
        border: 2px solid var(--ctp-surface0);
        border-radius: 6px;
        text-align: center;
        background: var(--ctp-base);
        color: var(--ctp-text);
    }

    .extend-inputs input:focus {
        border-color: var(--ctp-flamingo);
        outline: none;
    }

    .extend-inputs span {
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
    }

    .return-section {
        background: color-mix(in srgb, var(--ctp-blue) 8%, transparent);
        border: 1px solid color-mix(in srgb, var(--ctp-blue) 30%, transparent);
        border-radius: 8px;
        padding: 12px 15px;
        margin-top: 15px;
    }

    .return-section p {
        margin: 4px 0;
    }

    .return-status {
        color: var(--link);
        font-weight: 600;
    }

    .return-notice {
        color: var(--warn);
        font-weight: 600;
        font-size: 0.95rem;
    }

    .issue-section {
        background: color-mix(in srgb, var(--warn) 8%, transparent);
        border-color: color-mix(in srgb, var(--warn) 35%, transparent);
    }

    .issue-by {
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
        margin-bottom: 10px !important;
    }

    .found-section {
        background: color-mix(in srgb, var(--ctp-green) 8%, transparent);
        border: 1px solid color-mix(in srgb, var(--ctp-green) 30%, transparent);
        border-radius: 8px;
        padding: 15px;
        margin-top: 15px;
    }

    .found-notice {
        color: var(--ctp-green);
        font-weight: 600;
        margin: 0 0 10px;
        font-size: 0.95rem;
    }

    .found-actions {
        display: flex;
        gap: 10px;
        flex-wrap: wrap;
    }

    .found-btn {
        background: var(--ctp-green);
        color: var(--rp-base);
        border: none;
        min-height: 42px;
        padding: 8px clamp(14px, 2.5vw, 20px);
        border-radius: 6px;
        cursor: pointer;
        font: inherit;
        font-size: 0.9rem;
        font-weight: 600;
        transition: all 0.3s ease;
        box-shadow: var(--shadow-sm);
    }

    .found-btn:hover {
        filter: brightness(1.08);
        transform: translateY(-2px);
    }

    .found-btn:disabled {
        opacity: 0.6;
        cursor: not-allowed;
        transform: none;
    }

    .overdue-text {
        color: var(--danger);
        font-weight: 600;
    }

    .overdue-returned {
        margin: 8px 0;
    }

    .overdue-mark {
        color: var(--warn);
        font-weight: 600;
        background: color-mix(in srgb, var(--warn) 14%, transparent);
        padding: 2px 6px;
        border-radius: 4px;
        border: 1px solid color-mix(in srgb, var(--warn) 30%, transparent);
    }

    .subtitle-text {
        color: var(--ctp-subtext0);
        font-style: italic;
        margin: 0 0 20px;
        font-size: clamp(0.85rem, 1.8vw, 0.92rem);
    }

    .no-items {
        text-align: center;
        color: var(--ctp-subtext0);
        font-style: italic;
        padding: clamp(24px, 6vw, 50px);
        font-size: clamp(1rem, 2vw, 1.1rem);
    }

    .calendar-link {
        color: var(--link);
        text-decoration: none;
        margin-left: 6px;
    }

    .calendar-link:hover {
        text-decoration: underline;
    }

    .link-btn {
        background: none;
        border: none;
        padding: 0;
        font: inherit;
        color: var(--link);
        cursor: pointer;
    }

    .link-btn:hover {
        text-decoration: underline;
    }

    .more-btn {
        display: block;
        margin: 12px auto 0;
        min-height: 40px;
        padding: 0 12px;
    }

    /* --- printer cards --- */
    .printer-admin-grid {
        display: grid;
        /* Three printers across the 1200px column on a desktop, two on a
           tablet, one on a phone; min() stops a column overflowing a phone */
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 320px), 1fr));
        align-items: start;
        gap: 14px;
    }

    .printer-admin-card {
        --state: var(--ctp-overlay0);
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--ctp-text) 3.5%, transparent), transparent 55%),
            var(--ctp-mantle);
        border: 1px solid var(--ctp-surface0);
        border-left: 4px solid var(--state);
        border-radius: var(--radius-lg);
        padding: 16px;
        box-shadow: var(--shadow-sm);
        min-width: 0;
        transition:
            transform var(--normal) var(--ease),
            box-shadow var(--normal) var(--ease);
        animation: rise var(--slow) var(--ease) both;
        animation-delay: calc(var(--i, 0) * 70ms);
    }

    .printer-admin-card:hover {
        box-shadow: var(--shadow);
    }

    .printer-admin-card.running { --state: var(--ctp-green); }
    .printer-admin-card.failed { --state: var(--ctp-red); }
    .printer-admin-card.paused { --state: var(--ctp-yellow); }
    .printer-admin-card.finished { --state: var(--ctp-blue); }
    .printer-admin-card.idle { --state: var(--ctp-sapphire); }
    .printer-admin-card.offline .pa-head,
    .printer-admin-card.offline .pa-camera { opacity: 0.62; }

    .pa-head {
        display: flex;
        align-items: center;
        gap: 10px;
        margin-bottom: 12px;
    }

    .pa-image {
        width: 40px;
        height: 40px;
        object-fit: contain;
        flex-shrink: 0;
        transition: transform var(--normal) var(--ease);
    }

    .printer-admin-card:hover .pa-image {
        transform: scale(1.1) rotate(-3deg);
    }

    .pa-title {
        flex: 1;
        min-width: 0;
    }

    .pa-title h3 {
        margin: 0;
        font-size: 1rem;
        color: var(--ctp-text);
    }

    .pa-avail {
        font-size: 0.8rem;
        font-weight: 650;
    }

    .pa-avail.free { color: var(--ctp-green); }
    .pa-avail.busy { color: var(--ctp-peach); }
    .pa-avail.unknown { color: var(--ctp-overlay0); }

    .printer-admin-card.running .pa-avail.busy::before {
        content: '';
        display: inline-block;
        width: 7px;
        height: 7px;
        margin-right: 6px;
        border-radius: 50%;
        background: var(--ctp-peach);
        animation: soft-pulse 1.8s ease-in-out infinite;
    }

    .pa-state {
        font-size: 0.72rem;
        padding: 4px 10px;
        border-radius: var(--radius-pill);
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        white-space: nowrap;
    }

    .pa-state.running { background: var(--ctp-green); color: var(--rp-base); }
    .pa-state.failed,
    .pa-state.offline { background: var(--ctp-red); color: var(--accent-ink); }
    .pa-state.paused { background: var(--ctp-yellow); color: var(--rp-base); }
    .pa-state.finished { background: var(--ctp-blue); color: var(--accent-ink); }

    .pa-camera {
        aspect-ratio: 16 / 9;
        background: var(--ctp-crust);
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius);
        overflow: hidden;
        margin-bottom: 12px;
    }

    .pa-camera img {
        width: 100%;
        height: 100%;
        object-fit: cover;
        display: block;
        animation: fade var(--normal) var(--ease);
    }

    .pa-camera-empty {
        width: 100%;
        height: 100%;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        color: var(--ctp-overlay0);
    }

    /* Beat the ".pa-camera img" rule above, which would stretch the icon */
    .pa-camera-empty img {
        width: 60px;
        height: 60px;
        object-fit: contain;
        opacity: 0.35;
    }

    .pa-camera-empty p {
        margin: 4px 0 0;
        font-size: 0.78rem;
    }

    .pa-progress-row {
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .pa-progress-bar {
        flex: 1;
        height: 8px;
        background: var(--ctp-surface0);
        border-radius: var(--radius-pill);
        overflow: hidden;
    }

    .pa-progress-fill {
        height: 100%;
        border-radius: var(--radius-pill);
        background: linear-gradient(90deg, var(--ctp-green), var(--ctp-teal));
        transition: width var(--slow) var(--ease);
    }

    .pa-progress-text {
        font-size: 0.8rem;
        color: var(--ctp-subtext0);
        min-width: 36px;
        text-align: right;
    }

    .pa-remaining {
        margin: 6px 0 0;
        font-size: 0.85rem;
        color: var(--ctp-green);
    }

    .pa-file {
        margin: 8px 0 0;
        font-size: 0.8rem;
        color: var(--ctp-subtext0);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .pa-temps {
        display: flex;
        flex-wrap: wrap;
        gap: 10px;
        margin-top: 10px;
        font-size: 0.78rem;
        color: var(--ctp-subtext0);
    }

    /* --- AMS --- */
    .pa-ams {
        margin-top: 12px;
        padding: 10px;
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-sm);
        background: var(--ctp-crust);
    }

    .pa-ams-head {
        display: flex;
        align-items: center;
        gap: 10px;
        flex-wrap: wrap;
        margin-bottom: 8px;
    }

    .pa-ams-title {
        font-size: 0.72rem;
        font-weight: 700;
        letter-spacing: 0.6px;
        text-transform: uppercase;
        color: var(--ctp-teal);
    }

    .pa-ams-meta {
        font-size: 0.7rem;
        color: var(--ctp-subtext0);
    }

    .pa-ams-slots {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(68px, 1fr));
        gap: 6px;
    }

    .pa-ams-slot {
        display: flex;
        flex-direction: column;
        gap: 4px;
        padding: 7px;
        border: 1px solid var(--ctp-surface1);
        border-radius: var(--radius-sm);
        background: var(--ctp-mantle);
        min-width: 0;
    }

    .pa-ams-slot.active {
        border-color: var(--ctp-green);
        box-shadow: 0 0 0 1px var(--ctp-green) inset;
    }

    .pa-ams-slot.empty { opacity: 0.45; }

    .pa-ams-top {
        display: flex;
        align-items: center;
        gap: 5px;
    }

    .pa-ams-swatch {
        width: 14px;
        height: 14px;
        border-radius: 50%;
        border: 1px solid var(--ctp-surface2);
        flex-shrink: 0;
    }

    .pa-ams-num {
        font-size: 0.65rem;
        color: var(--ctp-overlay0);
        font-weight: 700;
    }

    .pa-ams-dot {
        width: 6px;
        height: 6px;
        border-radius: 50%;
        background: var(--ctp-green);
        margin-left: auto;
        animation: soft-pulse 1.8s ease-in-out infinite;
    }

    .pa-ams-mat {
        font-size: 0.68rem;
        color: var(--ctp-text);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .pa-ams-bar {
        display: block;
        height: 4px;
        background: var(--ctp-surface0);
        border-radius: var(--radius-pill);
        overflow: hidden;
    }

    .pa-ams-fill {
        display: block;
        height: 100%;
        background: linear-gradient(90deg, var(--ctp-teal), var(--ctp-sky));
        transition: width var(--slow) var(--ease);
    }

    .pa-ams-pct {
        font-size: 0.62rem;
        color: var(--ctp-subtext0);
    }

    .pa-ams-pct.unknown { color: var(--ctp-overlay0); }

    .pa-ams-ext {
        display: flex;
        align-items: center;
        gap: 7px;
        margin-top: 8px;
        padding-top: 8px;
        border-top: 1px dashed var(--ctp-surface1);
        font-size: 0.7rem;
    }

    .pa-ams-ext-label {
        color: var(--ctp-overlay0);
        text-transform: uppercase;
        letter-spacing: 0.4px;
        font-size: 0.62rem;
    }

    .pa-ams-ext-mat {
        color: var(--ctp-text);
        flex: 1;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    /* --- printer controls --- */
    .pa-stop {
        margin-top: 12px;
        width: 100%;
        min-height: 44px;
        background: var(--ctp-red);
        color: var(--accent-ink);
        border: none;
        border-radius: var(--radius-sm);
        font: inherit;
        font-weight: 650;
        font-size: 0.9rem;
        cursor: pointer;
        transition:
            filter var(--fast) var(--ease),
            transform var(--fast) var(--ease);
    }

    .pa-stop:hover { filter: brightness(1.08); transform: translateY(-1px); }
    .pa-stop:disabled { opacity: 0.6; cursor: not-allowed; }

    .pa-actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
        margin-top: 12px;
    }

    .pa-light,
    .pa-pause,
    .pa-resume {
        flex: 1;
        min-width: 110px;
        min-height: 44px;
        border: 1px solid var(--ctp-surface1);
        border-radius: var(--radius-sm);
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        font-weight: 600;
        font-family: inherit;
        font-size: 0.85rem;
        cursor: pointer;
        transition:
            background-color var(--fast) var(--ease),
            border-color var(--fast) var(--ease),
            transform var(--fast) var(--ease);
    }

    .pa-light:hover,
    .pa-pause:hover,
    .pa-resume:hover { transform: translateY(-1px); background: var(--ctp-surface1); }

    .pa-light.on {
        background: color-mix(in srgb, var(--ctp-yellow) 22%, var(--ctp-surface0));
        border-color: var(--ctp-yellow);
        color: var(--ctp-yellow);
    }

    .pa-pause { border-color: var(--ctp-yellow); color: var(--ctp-yellow); }
    .pa-resume { border-color: var(--ctp-green); color: var(--ctp-green); }

    .pa-light:disabled,
    .pa-pause:disabled,
    .pa-resume:disabled { opacity: 0.6; cursor: not-allowed; }

    /* --- HMS faults --- */
    .pa-faults {
        background: color-mix(in srgb, var(--ctp-red) 12%, transparent);
        border: 1px solid var(--ctp-red);
        border-radius: var(--radius-sm);
        padding: 10px 12px;
        margin-bottom: 12px;
        font-size: 0.82rem;
        color: var(--ctp-red);
    }

    .pa-fault {
        display: flex;
        align-items: center;
        gap: 8px;
        margin-top: 6px;
        min-height: 40px;
        padding: 6px 8px;
        border-radius: 6px;
        background: var(--ctp-crust);
        text-decoration: none;
        color: var(--ctp-subtext0);
        font-size: 0.75rem;
        transition: background-color var(--fast) var(--ease);
    }

    .pa-fault:hover { background: var(--ctp-surface0); }

    .pa-fault-sev {
        text-transform: uppercase;
        font-weight: 700;
        letter-spacing: 0.4px;
        padding: 2px 6px;
        border-radius: var(--radius-pill);
        background: var(--ctp-surface1);
        color: var(--ctp-text);
        font-size: 0.65rem;
    }

    .pa-fault.fatal .pa-fault-sev { background: var(--ctp-red); color: var(--accent-ink); }
    .pa-fault.serious .pa-fault-sev { background: var(--ctp-peach); color: var(--rp-base); }
    .pa-fault.common .pa-fault-sev { background: var(--ctp-yellow); color: var(--rp-base); }
    .pa-fault.info .pa-fault-sev { background: var(--ctp-sapphire); color: var(--accent-ink); }

    .pa-fault-code {
        flex: 1;
        font-family: var(--mono);
        color: var(--ctp-text);
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .pa-fault-link { color: var(--link); white-space: nowrap; }

    .pa-warning {
        background: color-mix(in srgb, var(--ctp-peach) 12%, transparent);
        border: 1px solid var(--ctp-peach);
        border-radius: var(--radius-sm);
        padding: 10px 12px;
        margin-bottom: 12px;
        font-size: 0.82rem;
        color: var(--ctp-yellow);
    }

    .pa-warning p {
        margin: 6px 0 0;
        color: var(--ctp-subtext0);
    }

    .pa-code-form {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        margin-top: 8px;
    }

    .pa-code-form input {
        flex: 1;
        min-width: 140px;
        min-height: 40px;
        background: var(--ctp-base);
        border: 1px solid var(--ctp-surface1);
        border-radius: 6px;
        padding: 8px;
        color: var(--ctp-text);
    }

    .pa-fix,
    .pa-save {
        background: var(--ctp-peach);
        color: var(--rp-base);
        border: none;
        border-radius: 6px;
        padding: 8px 14px;
        font: inherit;
        font-weight: 650;
        cursor: pointer;
        min-height: 40px;
        margin-top: 8px;
        transition: filter var(--fast) var(--ease), transform var(--fast) var(--ease);
    }

    .pa-save { margin-top: 0; }
    .pa-fix:hover, .pa-save:hover { filter: brightness(1.08); transform: translateY(-1px); }
    .pa-save:disabled { opacity: 0.6; cursor: not-allowed; }

    .pa-cancel {
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        border: none;
        border-radius: 6px;
        padding: 8px 12px;
        font: inherit;
        cursor: pointer;
        min-height: 40px;
    }

    /* --- each printer's own files --- */
    .pa-send-toggle {
        margin-top: 10px;
        width: 100%;
        min-height: 44px;
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        border: 1px solid var(--ctp-surface1);
        border-radius: var(--radius-sm);
        font-weight: 600;
        font-family: inherit;
        font-size: 0.85rem;
        cursor: pointer;
        transition: background-color var(--fast) var(--ease);
    }

    .pa-send-toggle:hover { background: var(--ctp-surface1); }

    .pa-send {
        margin-top: 10px;
        padding: 12px;
        border: 1px solid color-mix(in srgb, var(--state) 45%, var(--ctp-surface0));
        border-radius: var(--radius-sm);
        background: var(--ctp-crust);
        animation: rise var(--normal) var(--ease) both;
    }

    .pa-send-head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 8px;
        margin-bottom: 8px;
        padding-bottom: 8px;
        border-bottom: 1px solid var(--ctp-surface0);
    }

    .pa-send-title {
        font-weight: 700;
        font-size: 0.85rem;
        color: var(--ctp-text);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .pa-refresh {
        flex: none;
        min-height: 40px;
        padding: 0 12px;
        border-radius: 6px;
        border: 1px solid var(--ctp-surface1);
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        font: inherit;
        font-size: 0.78rem;
        cursor: pointer;
    }

    .pa-refresh:hover { background: var(--ctp-surface1); }
    .pa-refresh:disabled { opacity: 0.6; cursor: progress; }

    .pa-send-hint {
        margin: 0 0 10px;
        font-size: 0.75rem;
        color: var(--ctp-subtext0);
        line-height: 1.4;
    }

    .pa-file-pick {
        display: flex;
        align-items: center;
        justify-content: center;
        min-height: 48px;
        border: 1px dashed var(--ctp-surface2);
        border-radius: var(--radius-sm);
        padding: 12px;
        text-align: center;
        font-size: 0.82rem;
        color: var(--ctp-subtext0);
        cursor: pointer;
        transition: border-color var(--fast) var(--ease), color var(--fast) var(--ease);
    }

    .pa-file-pick:hover,
    .pa-file-pick:focus-within {
        border-color: var(--ctp-mauve);
        color: var(--ctp-text);
    }

    /* Hidden but still focusable from the keyboard */
    .pa-file-pick input {
        position: absolute;
        width: 1px;
        height: 1px;
        opacity: 0;
    }

    .pa-upload-bar {
        height: 6px;
        background: var(--ctp-surface0);
        border-radius: var(--radius-pill);
        overflow: hidden;
        margin-top: 8px;
    }

    .pa-upload-fill {
        height: 100%;
        background: linear-gradient(90deg, var(--ctp-mauve), var(--ctp-lavender));
        transition: width var(--fast) linear;
    }

    .pa-send-empty,
    .pa-file-summary {
        margin: 10px 0 0;
        font-size: 0.75rem;
        color: var(--ctp-overlay0);
    }

    .pa-send-error {
        margin: 10px 0 0;
        padding: 8px 10px;
        border-radius: 6px;
        background: color-mix(in srgb, var(--danger) 12%, transparent);
        color: var(--danger);
        font-size: 0.78rem;
    }

    .pa-file-list {
        list-style: none;
        margin: 6px 0 0;
        padding: 0;
        display: flex;
        flex-direction: column;
        gap: 4px;
    }

    .pa-file-list li {
        display: flex;
        align-items: center;
        gap: 8px;
        font-size: 0.78rem;
        background: var(--ctp-mantle);
        border: 1px solid var(--ctp-surface0);
        border-radius: 6px;
        padding: 4px 4px 4px 8px;
    }

    .pa-file-icon {
        flex: none;
    }

    .pa-file-main {
        flex: 1;
        min-width: 0;
        display: flex;
        flex-direction: column;
    }

    .pa-file-name {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        color: var(--ctp-text);
    }

    .pa-file-size {
        color: var(--ctp-overlay0);
        font-size: 0.7rem;
    }

    .pa-file-btn {
        flex: none;
        width: 40px;
        height: 40px;
        display: grid;
        place-items: center;
        background: none;
        border: 1px solid transparent;
        cursor: pointer;
        font-size: 0.9rem;
        border-radius: 6px;
        color: var(--ctp-text);
        transition: background-color var(--fast) var(--ease), border-color var(--fast) var(--ease);
    }

    .pa-file-btn.print { color: var(--ctp-green); }
    .pa-file-btn.print:hover { background: color-mix(in srgb, var(--ctp-green) 14%, transparent); border-color: var(--ctp-green); }
    .pa-file-btn.del:hover { background: color-mix(in srgb, var(--danger) 14%, transparent); border-color: var(--danger); }

    .pa-clear-all {
        width: 100%;
        margin-top: 10px;
        padding: 9px;
        border-radius: 8px;
        border: 1px solid var(--ctp-red);
        background: transparent;
        color: var(--ctp-red);
        font: inherit;
        font-size: 0.8rem;
        font-weight: 600;
        cursor: pointer;
        min-height: 42px;
    }

    .pa-clear-all:hover { background: var(--ctp-red); color: var(--accent-ink); }
    .pa-clear-all:disabled { opacity: 0.5; cursor: not-allowed; }

    /* --- starting a print --- */
    .pa-start-toggle {
        width: 100%;
        margin-top: 8px;
        padding: 9px;
        border-radius: 8px;
        border: 1px solid var(--ctp-green);
        background: transparent;
        color: var(--ctp-green);
        font: inherit;
        font-weight: 600;
        cursor: pointer;
        min-height: 44px;
    }

    .pa-start-toggle:hover { background: color-mix(in srgb, var(--ctp-green) 12%, transparent); }

    .pa-start {
        margin-top: 10px;
        padding: 12px;
        border-radius: 10px;
        background: var(--ctp-crust);
        border: 1px solid var(--ctp-surface0);
        display: flex;
        flex-direction: column;
        gap: 10px;
        animation: rise var(--normal) var(--ease) both;
    }

    .pa-start-row {
        display: flex;
        flex-direction: column;
        gap: 4px;
        font-size: 0.78rem;
        color: var(--ctp-subtext0);
    }

    .pa-start-row select,
    .pa-start-row input {
        padding: 8px;
        border-radius: 8px;
        border: 1px solid var(--ctp-surface1);
        background: var(--ctp-base);
        color: var(--ctp-text);
        font-size: 0.88rem;
        min-height: 42px;
        width: 100%;
    }

    .pa-swatches {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
    }

    .pa-swatch {
        width: 40px;
        height: 40px;
        border-radius: 8px;
        border: 2px solid var(--ctp-surface1);
        color: var(--rp-base);
        font-weight: 700;
        font-size: 0.75rem;
        cursor: pointer;
        text-shadow: 0 0 3px rgba(255, 255, 255, 0.7);
    }

    .pa-swatch.picked {
        border-color: var(--ctp-green);
        transform: scale(1.06);
        box-shadow: 0 0 0 2px color-mix(in srgb, var(--ctp-green) 40%, transparent);
    }

    .pa-start-opts {
        display: flex;
        flex-wrap: wrap;
        gap: 4px 14px;
        font-size: 0.8rem;
        color: var(--ctp-subtext0);
    }

    .pa-start-opts label {
        display: flex;
        align-items: center;
        gap: 6px;
        min-height: 40px;
    }

    .pa-start-warn {
        margin: 0;
        font-size: 0.75rem;
        color: var(--ctp-yellow);
    }

    .pa-start-go {
        padding: 11px;
        border-radius: 8px;
        border: none;
        background: var(--ctp-green);
        color: var(--rp-base);
        font: inherit;
        font-weight: 700;
        cursor: pointer;
        min-height: 44px;
    }

    .pa-start-go:disabled {
        opacity: 0.5;
        cursor: not-allowed;
    }

    .pa-last,
    .pa-offline {
        margin: 8px 0 0;
        font-size: 0.75rem;
        color: var(--ctp-overlay0);
    }

    input[type='checkbox'] {
        accent-color: var(--accent);
        width: 18px;
        height: 18px;
    }

    /* --- print log --- */
    .joblog-head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 10px;
        margin-top: 30px;
        flex-wrap: wrap;
    }

    .joblog-head h3 {
        margin: 0;
        font-size: 1.05rem;
    }

    .joblog-tools {
        display: flex;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
    }

    .joblog-tools select {
        min-height: 40px;
        padding: 6px 10px;
    }

    .joblog-head + .subtitle-text {
        margin-top: 6px;
    }

    .joblog {
        display: flex;
        flex-direction: column;
        gap: 6px;
        margin-top: 10px;
    }

    .joblog-row {
        display: grid;
        grid-template-columns: 110px 90px minmax(0, 1fr) 70px 150px;
        gap: 10px;
        align-items: center;
        background: var(--ctp-mantle);
        border: 1px solid var(--ctp-surface0);
        border-left: 3px solid var(--ctp-overlay0);
        border-radius: var(--radius-sm);
        padding: 8px 12px;
        font-size: 0.8rem;
    }

    .joblog-row.finished { border-left-color: var(--ctp-green); }
    .joblog-row.failed { border-left-color: var(--ctp-red); }
    .joblog-row.stopped { border-left-color: var(--ctp-peach); }
    .joblog-row.running { border-left-color: var(--ctp-blue); }

    .joblog-when { color: var(--ctp-subtext0); }
    .joblog-printer { color: var(--ctp-mauve); font-weight: 600; }

    .joblog-file {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .joblog-dur { color: var(--ctp-subtext0); }

    .joblog-result {
        font-weight: 600;
        text-align: right;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .joblog-result.finished { color: var(--ctp-green); }
    .joblog-result.failed { color: var(--ctp-red); }
    .joblog-result.stopped { color: var(--ctp-peach); }
    .joblog-result.running { color: var(--link); }

    /* --- bookings --- */
    .embedded-calendar {
        margin-bottom: 24px;
    }

    .list-head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        flex-wrap: wrap;
        gap: 10px;
        margin: 24px 0 12px;
    }

    .cal-list-title {
        font-size: 1.05rem;
        color: var(--ctp-subtext0);
        margin: 0;
    }

    .bookings-list {
        display: flex;
        flex-direction: column;
        gap: 10px;
    }

    .booking-row {
        display: flex;
        align-items: center;
        gap: 14px;
        background: var(--ctp-crust);
        border: 1px solid var(--ctp-surface0);
        border-left: 4px solid var(--res, var(--ctp-mauve));
        border-radius: 8px;
        padding: 12px 14px;
        flex-wrap: wrap;
    }

    .booking-row.pending {
        background: color-mix(in srgb, var(--warn) 6%, var(--ctp-crust));
    }

    .booking-row.past {
        opacity: 0.55;
        border-left-color: var(--ctp-overlay0);
    }

    .booking-when {
        display: flex;
        flex-direction: column;
        min-width: 150px;
    }

    .booking-date {
        font-weight: 600;
        color: var(--ctp-mauve);
    }

    .booking-time {
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
    }

    .booking-info {
        flex: 1;
        min-width: 180px;
    }

    .booking-purpose {
        margin: 0;
        font-weight: 500;
    }

    .booking-by {
        margin: 2px 0 0;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
    }

    .booking-res {
        color: var(--ctp-text);
        font-weight: 600;
    }

    .booking-actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
        align-items: center;
    }

    .tag-row {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        margin-top: 6px;
    }

    .tag {
        padding: 2px 8px;
        border-radius: var(--radius-pill);
        background: var(--ctp-surface0);
        color: var(--ctp-subtext0);
        font-size: 0.72rem;
        font-weight: 600;
    }

    .tag.warn { background: color-mix(in srgb, var(--warn) 16%, transparent); color: var(--warn); }
    .tag.danger { background: color-mix(in srgb, var(--danger) 16%, transparent); color: var(--danger); }
    .tag.ok { background: color-mix(in srgb, var(--ctp-green) 16%, transparent); color: var(--ctp-green); }

    /* --- approvals --- */
    .approval-title {
        margin: 26px 0 0;
        font-size: 1.1rem;
        color: var(--ctp-text);
    }

    .approval-title:first-of-type {
        margin-top: 8px;
    }

    .approval-title + .no-items {
        padding: 18px;
    }

    .approval-title + .bookings-list {
        margin-top: 14px;
    }

    .own-note {
        margin: 0;
        font-size: 0.85rem;
        font-style: italic;
        color: var(--ctp-subtext0);
    }

    /* --- history --- */
    .history-search-container {
        background: var(--ctp-crust);
        padding: 20px;
        border-radius: 12px;
        margin-bottom: 20px;
        border: 1px solid var(--ctp-surface0);
        box-shadow: var(--shadow-sm);
    }

    .history-search-container label {
        display: block;
        margin-bottom: 10px;
        font-weight: 600;
        color: var(--ctp-text);
        font-size: 1rem;
    }

    .search-input-group {
        display: flex;
        gap: 8px;
        align-items: center;
    }

    .history-search-input {
        flex: 1;
        width: 100%;
        min-width: 0;
        min-height: 44px;
        padding: 10px 16px;
        border: 2px solid var(--ctp-surface0);
        border-radius: 8px;
        font-size: 1rem;
        background: var(--ctp-base);
        color: var(--ctp-text);
        transition: all 0.3s ease;
    }

    .history-search-input:focus {
        border-color: var(--ctp-flamingo);
        outline: none;
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--ctp-flamingo) 25%, transparent);
    }

    .clear-search-btn {
        flex: none;
        width: 44px;
        height: 44px;
        border-radius: 8px;
        border: 1px solid var(--ctp-surface1);
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        cursor: pointer;
        font-size: 1rem;
    }

    .clear-search-btn:hover { background: var(--ctp-surface1); }

    .history-filters {
        margin-top: 12px;
    }

    .search-results-info {
        margin: 10px 0 0;
        color: var(--ctp-subtext0);
        font-size: 0.9rem;
        font-style: italic;
    }

    .no-search-results {
        text-align: center;
        padding: 40px 20px;
        background: var(--ctp-crust);
        border-radius: 12px;
        border: 1px solid var(--ctp-surface0);
        margin-top: 20px;
    }

    .no-search-results p {
        color: var(--ctp-text);
        font-size: 1.1rem;
        margin: 0 0 15px;
    }

    .clear-search-btn-large {
        background: linear-gradient(135deg, var(--ctp-sapphire), var(--ctp-teal));
        color: var(--accent-ink);
        border: none;
        min-height: 44px;
        padding: 10px 20px;
        border-radius: 8px;
        font: inherit;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.3s ease;
    }

    .clear-search-btn-large:hover {
        filter: brightness(1.06);
        transform: translateY(-1px);
    }

    .history-list {
        display: flex;
        flex-direction: column;
        gap: 20px;
        margin-top: 20px;
        position: relative;
    }

    .history-item {
        display: flex;
        background: var(--ctp-crust);
        border-radius: 12px;
        box-shadow: var(--shadow);
        border: 1px solid var(--ctp-surface0);
        transition: all 0.3s ease;
        position: relative;
        min-width: 0;
    }

    .history-item:hover {
        transform: translateY(-2px);
        box-shadow: var(--shadow-lg);
        border-color: var(--ctp-flamingo);
    }

    .history-item.active { border-left: 5px solid var(--ctp-green); }
    .history-item.returned { border-left: 5px solid var(--ctp-teal); }
    .history-item.not_found { border-left: 5px solid var(--ctp-red); }
    .history-item.denied { border-left: 5px solid var(--ctp-overlay0); }
    .history-item.pending { border-left: 5px solid var(--warn); }

    .history-timeline {
        display: flex;
        flex-direction: column;
        align-items: center;
        width: 52px;
        flex: none;
        padding: 20px 0;
    }

    .timeline-dot {
        width: 16px;
        height: 16px;
        border-radius: 50%;
        border: 3px solid var(--ctp-surface0);
        margin-top: 5px;
    }

    .timeline-dot.active { background: var(--ctp-green); border-color: var(--ctp-green); }
    .timeline-dot.returned { background: var(--ctp-teal); border-color: var(--ctp-teal); }
    .timeline-dot.not_found { background: var(--ctp-red); border-color: var(--ctp-red); }
    .timeline-dot.denied { background: var(--ctp-overlay0); border-color: var(--ctp-overlay0); }
    .timeline-dot.pending { background: var(--warn); border-color: var(--warn); }

    .timeline-line {
        width: 2px;
        flex: 1;
        background: var(--ctp-surface0);
        margin-top: 10px;
    }

    .history-item:last-child .timeline-line {
        display: none;
    }

    .history-content {
        flex: 1;
        min-width: 0;
        padding: 20px 25px 20px 0;
    }

    .history-header {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        margin-bottom: 16px;
        gap: 15px;
        flex-wrap: wrap;
    }

    .history-title-section {
        display: flex;
        align-items: flex-start;
        gap: 15px;
        flex: 1;
        min-width: 0;
    }

    .history-title-section h3 {
        color: var(--ctp-text);
        margin: 0;
        font-size: clamp(1.1rem, 2.5vw, 1.3rem);
        font-weight: 600;
        line-height: 1.2;
        overflow-wrap: anywhere;
    }

    .history-status-badges {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
    }

    .history-date {
        display: flex;
        flex-direction: column;
        align-items: flex-end;
        text-align: right;
        min-width: 120px;
    }

    .date-label {
        font-size: 0.8rem;
        color: var(--ctp-subtext0);
        font-weight: 500;
    }

    .date-value {
        font-size: 0.9rem;
        color: var(--ctp-text);
        font-weight: 600;
        margin-top: 2px;
    }

    .detail-row {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 200px), 1fr));
        gap: 16px;
    }

    .detail-group {
        display: flex;
        flex-direction: column;
        gap: 8px;
        min-width: 0;
    }

    .detail-group p {
        margin: 0;
        color: var(--ctp-subtext0);
        font-size: clamp(0.88rem, 1.8vw, 0.95rem);
        overflow-wrap: anywhere;
    }

    .detail-group strong {
        color: var(--ctp-text);
        font-weight: 600;
    }

    /* --- item catalogue --- */
    .item-card.has-issue {
        border-left-color: var(--warn);
    }

    .item-card.tracked:not(.has-issue) {
        border-left-color: var(--ctp-teal);
    }

    .item-actions {
        align-items: stretch;
    }

    .merge-group {
        display: flex;
        gap: 6px;
        flex: 1;
        min-width: 200px;
    }

    .merge-group select {
        flex: 1;
        min-width: 0;
        min-height: 42px;
        padding: 6px 8px;
    }

    .inline-form {
        margin-top: 4px;
        animation: rise var(--normal) var(--ease) both;
    }

    .full-row {
        grid-column: 1 / -1;
    }

    /* --- forms (change password, edits) --- */
    .form-container {
        padding: 20px 0;
        max-width: 600px;
        margin: 0 auto;
    }

    .form-card {
        background: var(--ctp-surface0);
        border-radius: 12px;
        padding: clamp(16px, 4vw, 24px);
        border: 1px solid var(--ctp-surface1);
        box-shadow: var(--shadow-sm);
    }

    .form-card h4,
    .form-title {
        margin: 0 0 12px;
        color: var(--ctp-text);
        font-size: 1.05rem;
    }

    .form-note,
    .section-note {
        margin: 0 0 16px;
        color: var(--ctp-subtext0);
        font-size: 0.9rem;
    }

    .form-group {
        margin-bottom: 16px;
        text-align: left;
    }

    .form-group label {
        display: block;
        margin-bottom: 8px;
        font-weight: 500;
        color: var(--ctp-text);
    }

    .form-group input:not([type='checkbox']),
    .form-group select {
        width: 100%;
        min-height: 44px;
        padding: 10px 14px;
        background: var(--ctp-base);
        border: 2px solid var(--ctp-surface1);
        border-radius: 8px;
        color: var(--ctp-text);
        font-size: 1rem;
        transition: all 0.2s ease;
    }

    .form-group input:not([type='checkbox']):focus,
    .form-group select:focus {
        outline: none;
        border-color: var(--ctp-blue);
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--ctp-blue) 15%, transparent);
    }

    .checkbox-label {
        display: flex !important;
        align-items: center;
        gap: 12px;
        min-height: 40px;
        margin-bottom: 0 !important;
        cursor: pointer;
    }

    .form-actions {
        display: flex;
        gap: 12px;
        margin-top: 20px;
        flex-wrap: wrap;
    }

    .submit-btn {
        background: linear-gradient(135deg, var(--ctp-blue), var(--ctp-sapphire));
        color: var(--accent-ink);
        border: none;
        min-height: 44px;
        padding: 10px 24px;
        border-radius: 8px;
        font: inherit;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.2s ease;
        flex: 1;
    }

    .submit-btn:hover:not(:disabled) {
        transform: translateY(-2px);
        box-shadow: 0 4px 12px color-mix(in srgb, var(--ctp-blue) 30%, transparent);
    }

    .submit-btn:disabled {
        opacity: 0.6;
        cursor: not-allowed;
        transform: none;
    }

    .cancel-btn {
        background: var(--ctp-overlay0);
        color: var(--accent-ink);
        border: none;
        min-height: 44px;
        padding: 10px 24px;
        border-radius: 8px;
        font: inherit;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.2s ease;
    }

    .cancel-btn:hover {
        filter: brightness(1.1);
        transform: translateY(-1px);
    }

    .as-link {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        text-decoration: none;
    }

    .no-access h2 {
        margin-bottom: 12px;
    }

    .no-access p {
        color: var(--ctp-subtext0);
        line-height: 1.5;
    }

    /* --- users & groups --- */
    .admin-management-container {
        padding: 0;
        max-width: 1200px;
        margin: 0 auto;
    }

    .admin-management-container > h2 {
        margin-bottom: 18px;
    }

    .management-section {
        background: var(--ctp-surface0);
        border-radius: 12px;
        padding: clamp(16px, 3vw, 24px);
        margin: 0 0 24px;
        border: 1px solid var(--ctp-surface1);
        min-width: 0;
    }

    .loans-content .management-section {
        margin-top: 8px;
    }

    .management-section.waiting {
        border-color: var(--warn);
        background: color-mix(in srgb, var(--warn) 7%, var(--ctp-surface0));
    }

    .section-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
        margin-bottom: 16px;
    }

    .section-header h3 {
        margin: 0;
        color: var(--ctp-text);
        font-size: 1.2rem;
    }

    .count-note {
        font-size: 0.8rem;
        font-weight: 500;
        color: var(--ctp-subtext0);
        margin-left: 6px;
    }

    .toggle-btn,
    .refresh-btn {
        border: none;
        min-height: 40px;
        padding: 8px 16px;
        border-radius: 6px;
        font: inherit;
        font-size: 0.9rem;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.2s ease;
    }

    .toggle-btn {
        background: var(--ctp-blue);
        color: var(--accent-ink);
    }

    .toggle-btn.subtle {
        background: var(--ctp-base);
        color: var(--ctp-text);
        border: 1px solid var(--ctp-surface1);
    }

    .refresh-btn {
        background: var(--ctp-green);
        color: var(--rp-base);
        flex: none;
    }

    .toggle-btn:hover,
    .refresh-btn:hover {
        filter: brightness(1.08);
        transform: translateY(-1px);
    }

    .user-tools {
        display: flex;
        flex-wrap: wrap;
        gap: 12px;
        align-items: center;
        margin-bottom: 16px;
    }

    .user-tools .history-search-input {
        flex: 1 1 260px;
    }

    .admin-list {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr));
        gap: 16px;
    }

    .admin-card {
        background: var(--ctp-base);
        border-radius: 8px;
        padding: 18px;
        border: 1px solid var(--ctp-surface1);
        transition: all 0.2s ease;
        display: flex;
        flex-direction: column;
        justify-content: space-between;
        min-width: 0;
    }

    .admin-card:hover,
    .admin-card.editing {
        border-color: var(--ctp-blue);
        box-shadow: 0 4px 12px color-mix(in srgb, var(--ctp-blue) 12%, transparent);
    }

    .admin-card.inactive {
        opacity: 0.65;
    }

    .admin-card.pending-card {
        border-color: color-mix(in srgb, var(--warn) 55%, var(--ctp-surface1));
    }

    .admin-card-info {
        min-width: 0;
    }

    .admin-card-info h4 {
        display: flex;
        align-items: center;
        gap: 8px;
        flex-wrap: wrap;
        margin: 0 0 8px 0;
        color: var(--ctp-text);
        font-size: 1.1rem;
    }

    .admin-username {
        margin: 4px 0;
        color: var(--link);
        font-weight: 500;
        overflow-wrap: anywhere;
    }

    .admin-phone {
        margin: 4px 0;
        color: var(--ctp-subtext0);
        font-size: 0.9rem;
    }

    .admin-role {
        display: flex;
        align-items: center;
        gap: 10px;
        flex-wrap: wrap;
        margin: 10px 0;
        font-weight: 600;
    }

    .admin-date {
        margin: 8px 0 0 0;
        color: var(--ctp-overlay0);
        font-size: 0.88rem;
    }

    .group-pick {
        display: inline-flex;
        align-items: center;
        gap: 8px;
        font-size: 0.85rem;
        font-weight: 500;
        color: var(--ctp-subtext0);
        margin-top: 8px;
    }

    .admin-role .group-pick {
        margin-top: 0;
    }

    .group-pick select {
        min-height: 40px;
        padding: 6px 10px;
    }

    .group-chip {
        padding: 4px 10px;
        border-radius: var(--radius-pill);
        background: var(--ctp-surface0);
        color: var(--ctp-mauve);
        font-size: 0.85rem;
    }

    .admin-card-actions {
        display: flex;
        justify-content: flex-end;
        align-items: center;
        gap: 8px;
        flex-wrap: wrap;
        margin-top: 16px;
        padding-top: 12px;
        border-top: 1px solid var(--ctp-surface1);
    }

    .delete-admin-btn {
        background: var(--ctp-red);
        color: var(--accent-ink);
        border: none;
        min-height: 40px;
        padding: 6px 14px;
        border-radius: 6px;
        font: inherit;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.2s ease;
        font-size: 0.9rem;
    }

    .delete-admin-btn:hover {
        filter: brightness(1.08);
        transform: translateY(-1px);
    }

    .current-user-badge {
        background: var(--ctp-green);
        color: var(--rp-base);
        padding: 2px 8px;
        border-radius: 4px;
        font-size: 0.75rem;
        font-weight: 700;
    }

    .override-card {
        background: var(--ctp-mantle);
        border-color: var(--ctp-blue);
        animation: rise var(--normal) var(--ease) both;
    }

    .override-grid {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr));
        gap: 8px 20px;
    }

    .override-row {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 10px;
        padding: 6px 0;
        border-bottom: 1px solid var(--ctp-surface0);
        font-size: 0.92rem;
    }

    .override-row select {
        flex: none;
        min-height: 40px;
        padding: 6px 8px;
        max-width: 55%;
    }

    .override-row select.grant { border-color: var(--ctp-green); color: var(--ctp-green); }
    .override-row select.revoke { border-color: var(--danger); color: var(--danger); }

    .table-wrap {
        overflow-x: auto;
        background: var(--ctp-base);
        border: 1px solid var(--ctp-surface1);
        border-radius: 8px;
    }

    .perm-table {
        width: 100%;
        border-collapse: collapse;
        font-size: 0.9rem;
    }

    .perm-table th,
    .perm-table td {
        padding: 10px 14px;
        border-bottom: 1px solid var(--ctp-surface0);
        text-align: left;
    }

    .perm-table thead th {
        color: var(--ctp-subtext0);
        font-weight: 600;
        white-space: nowrap;
        background: var(--ctp-mantle);
    }

    .perm-table tbody th {
        font-weight: 500;
        color: var(--ctp-text);
        min-width: 180px;
    }

    .perm-table tbody tr:hover {
        background: color-mix(in srgb, var(--ctp-surface0) 50%, transparent);
    }

    .perm-table .center {
        text-align: center;
    }

    .group-count {
        display: block;
        font-size: 0.72rem;
        font-weight: 500;
        color: var(--ctp-overlay0);
    }

    .danger-zone {
        border-color: var(--ctp-red) !important;
        background: color-mix(in srgb, var(--ctp-red) 8%, var(--ctp-surface0));
    }

    .danger-zone .section-header h3 {
        color: var(--ctp-red);
    }

    .danger-actions {
        display: flex;
        flex-direction: column;
        gap: 16px;
    }

    .danger-item {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 16px;
        padding: 16px;
        background: var(--ctp-base);
        border-radius: 8px;
        border: 1px solid var(--ctp-surface1);
    }

    .danger-info h4 {
        margin: 0 0 4px 0;
        color: var(--ctp-red);
    }

    .danger-info p {
        margin: 0;
        color: var(--ctp-text);
        font-size: 0.9rem;
    }

    .danger-btn {
        background: var(--ctp-red);
        color: var(--accent-ink);
        border: none;
        min-height: 44px;
        padding: 10px 20px;
        border-radius: 6px;
        font: inherit;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.2s ease;
        white-space: nowrap;
    }

    .danger-btn:hover {
        filter: brightness(1.08);
        transform: translateY(-1px);
    }

    .no-data {
        text-align: center;
        color: var(--ctp-overlay0);
        padding: 16px;
        margin: 0;
    }

    /* --- responsive --- */
    @media (min-width: 1200px) {
        .loans-grid {
            grid-template-columns: repeat(auto-fill, minmax(500px, 1fr));
        }

        .items-grid {
            grid-template-columns: repeat(auto-fill, minmax(400px, 1fr));
        }

        .overview-cards {
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
        }
    }

    @media (max-width: 768px) {
        .dashboard-header {
            gap: 10px;
            padding-bottom: 14px;
        }

        /* Full-bleed strip on phones, so tabs scroll from the screen edge */
        .nav-tabs {
            margin-left: -16px;
            margin-right: -16px;
            padding-left: 8px;
            padding-right: 8px;
        }

        .loan-title-section {
            flex-direction: column;
            gap: 10px;
        }

        .loan-actions {
            flex-direction: column;
            align-items: stretch;
        }

        .loan-actions > * {
            width: 100%;
        }

        .merge-group {
            min-width: 0;
        }

        .extend-inputs input {
            flex: 1;
            width: auto;
            min-width: 60px;
        }

        .export-btn {
            width: 100%;
        }

        .history-timeline {
            width: 32px;
        }

        .history-content {
            padding: 16px 16px 16px 0;
        }

        .history-date {
            align-items: flex-start;
            text-align: left;
        }

        .booking-actions {
            width: 100%;
        }

        .booking-actions > button {
            flex: 1;
        }

        .form-actions {
            flex-direction: column;
        }

        .danger-item {
            flex-direction: column;
            align-items: stretch;
        }

        .admin-card-actions {
            justify-content: stretch;
        }

        .admin-card-actions > button {
            flex: 1 1 auto;
        }

        .override-row {
            flex-direction: column;
            align-items: stretch;
        }

        .override-row select {
            max-width: none;
        }
    }

    @media (max-width: 700px) {
        .joblog-row {
            grid-template-columns: 1fr 1fr;
            row-gap: 4px;
        }

        .joblog-file { grid-column: 1 / -1; }
        .joblog-result { text-align: left; }
    }

    @media (max-width: 480px) {
        .loan-card {
            padding: 15px;
        }

        .status-filters {
            gap: 6px;
        }

        .filter-btn {
            padding: 6px 12px;
        }

        .history-search-container {
            padding: 14px;
        }
    }
</style>
