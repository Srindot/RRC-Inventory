<script>
    import { onMount, onDestroy } from 'svelte';
    import { replaceState } from '$app/navigation';
    import { cameraFeed } from '$lib/camera.js';
    import { api, can, clearSession as clearAuth, getToken, loginUrl, refreshUser } from '$lib/auth.js';
    import BookingPage from '$lib/BookingPage.svelte';
    import Icon from '$lib/Icon.svelte';
    import { AREA, hueFor, LAB } from '$lib/palette.js';

    // Authentication state
    let isLoggedIn = false;
    let adminInfo = null;
    let authToken = '';

    // Current view
    let currentView = 'dashboard'; // dashboard (Overview), printers, lost-missing, history, lab-view, users, items, bookings
    let selectedLab = '';
    let selectedFilter = 'all'; // all, borrowed, returned, not_found

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
    $: shownItems = catalogue.filter((i) => i.name.toLowerCase().includes(itemSearch.trim().toLowerCase()));
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
    let filteredHistoryItems = [];

    // Simple reactive statement for history search
    $: updateFilteredHistoryItems(historyItems, historySearchQuery);
    
    function updateFilteredHistoryItems(currentHistoryItems, currentSearchQuery) {
        if (!currentHistoryItems) {
            filteredHistoryItems = [];
            return;
        }
        
        let itemsToFilter = [...currentHistoryItems]; // Create a copy
        
        if (currentSearchQuery && currentSearchQuery.trim()) {
            const query = currentSearchQuery.toLowerCase().trim();
            itemsToFilter = currentHistoryItems.filter(item => {
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

    // Lab options
    const labs = ['Main Lab', 'Mech Lab', 'Control Lab'];
    // Printer state -> chip hue; 'finished' stays neutral (nothing to do but clear the plate)
    const PRINTER_HUE = { running: 'var(--ctp-green)', idle: 'var(--ctp-blue)', paused: 'var(--ctp-yellow)', failed: 'var(--danger)', offline: 'var(--danger)' };

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
        const tab = new URLSearchParams(location.search).get('tab');
        openTab(tab || (!can(adminInfo, 'manage_inventory') && can(adminInfo, 'manage_users') ? 'users' : 'overview'));
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
        else if (tab?.startsWith('lab:') && inv && labs.includes(tab.slice(4))) showLabView(tab.slice(4));
        else goToDashboard();
    }

    const TAB_PARAM = { dashboard: 'overview', 'lost-missing': 'missing' };
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
            const response = await apiFetch(`/api/admin/loans/by-lab/${encodeURIComponent(lab)}?status=${filter}`);
            if (response && response.ok) {
                labLoans = await response.json();
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
    $: resourceName = Object.fromEntries(resources.map((e) => [e.resource.ID, e.resource.name]));
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

    async function loadPrinters() {
        const response = await apiFetch('/api/printers');
        if (response && response.ok) {
            printers = await response.json();
        }
    }

    function showPrinters() {
        currentView = 'printers';
        loadPrinters();
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
    let uploadFor = '';
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
        if (uploadFor === printer.id) {
            uploadFor = '';
            return;
        }
        uploadFor = printer.id;
        loadPrinterFiles(printer);
    }

    async function loadPrinterFiles(printer) {
        const response = await apiFetch(`/api/printers/${printer.id}/files`);
        if (!response) return;
        if (response.ok) {
            uploadFiles = { ...uploadFiles, [printer.id]: await response.json() };
        } else {
            const error = await readJSON(response);
            showMessage(apiError(response, error, 'Could not list the printer files'), 'error');
        }
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

    async function loadPrintJobs() {
        const response = await apiFetch('/api/admin/print-jobs');
        if (response && response.ok) {
            printJobs = await response.json();
        }
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

    const groupName = (id) => groups.find((g) => g.id === id)?.name ?? '—';

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
    <div class="message {messageType}">{message}</div>
{/if}

<div class="page">
    <header class="page-head">
        <div class="head-title">
            <span class="icon-badge" style="--hue: {AREA.admin}"><Icon name="shield" /></span>
            <div>
                <h1>Admin</h1>
                <p>Loans, items, bookings, printers and people.</p>
            </div>
        </div>
    </header>

    {#if isLoggedIn && !hasAdminArea}
        <p class="muted">Nothing to manage here. Your dashboard is at <a href="/dashboard">/dashboard</a>.</p>
    {:else if isLoggedIn}
        <nav class="tabs" aria-label="Admin sections">
            <button class:active={currentView === 'dashboard'} on:click={goToDashboard}>Overview</button>
            {#if can(adminInfo, 'manage_inventory')}
                <button style="--hue: var(--warn)" class:active={currentView === 'lost-missing'} on:click={showLostMissingItems}>
                    <span class="dot"></span>Missing items
                    {#if lostMissingItems.length > 0}<span class="count mono">{lostMissingItems.length}</span>{/if}
                </button>
                {#each labs as lab}
                    <button style="--hue: {LAB[lab]}" class:active={currentView === 'lab-view' && selectedLab === lab} on:click={() => showLabView(lab)}><span class="dot"></span>{lab}</button>
                {/each}
            {/if}
            {#if can(adminInfo, 'manage_printers')}
                <button style="--hue: {AREA.printers}" class:active={currentView === 'printers'} on:click={showPrinters}><span class="dot"></span>Printers</button>
            {/if}
            {#if can(adminInfo, 'manage_bookings')}
                <button style="--hue: {AREA.mech}" class:active={currentView === 'bookings'} on:click={showBookings}><span class="dot"></span>Bookings</button>
            {/if}
            {#if can(adminInfo, 'manage_inventory')}
                <button style="--hue: var(--ctp-lavender)" class:active={currentView === 'history'} on:click={showItemHistory}><span class="dot"></span>Item history</button>
                <button style="--hue: {AREA.equipment}" class:active={currentView === 'items'} on:click={showItems}><span class="dot"></span>Items</button>
            {/if}
            {#if can(adminInfo, 'manage_users')}
                <button style="--hue: {AREA.admin}" class:active={currentView === 'users'} on:click={showUsers}>
                    <span class="dot"></span>Users
                    {#if pendingUsers.length > 0}<span class="count mono">{pendingUsers.length}</span>{/if}
                </button>
            {/if}
        </nav>

        <!-- Overview -->
        {#if currentView === 'dashboard'}
            <div class="grid">
                {#if can(adminInfo, 'manage_users')}
                    <!-- Solid only when someone is actually waiting: the one figure to act on -->
                    <button class="card link-card" class:filled={pendingUsers.length > 0} class:hued={pendingUsers.length === 0} style="--hue: {AREA.admin}" on:click={showUsers}>
                        <span class="icon-badge"><Icon name="users" /></span>
                        <p class="label">Pending sign-ups</p>
                        <p class="stat mono" class:hue-text={pendingUsers.length === 0}>{pendingUsers.length}</p>
                        <p class="card-note">Waiting for approval</p>
                    </button>
                {/if}
                {#if can(adminInfo, 'manage_inventory')}
                    <button class="card link-card hued" style="--hue: var(--warn)" on:click={showLostMissingItems}>
                        <span class="icon-badge"><Icon name="box" /></span>
                        <p class="label">Missing items</p>
                        <p class="stat mono hue-text">{lostMissingItems.length}</p>
                        <p class="card-note">Marked missing in a lab</p>
                    </button>
                    <button class="card link-card hued" style="--hue: {AREA.equipment}" on:click={showItems}>
                        <span class="icon-badge"><Icon name="box" /></span>
                        <p class="label">Items</p>
                        <p class="stat mono hue-text">{catalogue.length}</p>
                        <p class="card-note">In the catalogue</p>
                    </button>
                {/if}
                {#if can(adminInfo, 'manage_printers')}
                    <button class="card link-card hued" style="--hue: {AREA.printers}" on:click={showPrinters}>
                        <span class="icon-badge"><Icon name="printer" /></span>
                        <p class="label">3D printers</p>
                        <p class="card-note">Live status and print controls</p>
                    </button>
                {/if}
                {#if can(adminInfo, 'manage_bookings')}
                    <button class="card link-card hued" style="--hue: {AREA.mech}" on:click={showBookings}>
                        <span class="icon-badge"><Icon name="calendar" /></span>
                        <p class="label">Bookings</p>
                        <p class="card-note">Mocap Lab and Mech Lab robots</p>
                    </button>
                {/if}
                {#if can(adminInfo, 'manage_inventory')}
                    {#each labs as lab}
                        <button class="card link-card hued" style="--hue: {LAB[lab]}" on:click={() => showLabView(lab)}>
                            <span class="icon-badge"><Icon name="grid" /></span>
                            <p class="label">{lab}</p>
                            <p class="card-note">Borrowed items</p>
                        </button>
                    {/each}
                {/if}
            </div>
            <div class="actions export">
                {#if can(adminInfo, 'manage_inventory')}
                    <button class="btn" on:click={() => exportCSV()}>Export loans (CSV)</button>
                {/if}
                {#if can(adminInfo, 'manage_bookings')}
                    <button class="btn" on:click={() => exportCSV('/api/admin/export-bookings-csv', 'motion_capture_lab_bookings.csv')}>Export bookings (CSV)</button>
                {/if}
            </div>
        {/if}

        <!-- Missing items -->
        {#if currentView === 'lost-missing'}
            <section>
                <div class="view-head"><h2>Missing items</h2></div>
                <p class="intro muted">Items an admin could not find in the lab.</p>
                {#if loading}
                    <p class="muted">Loading…</p>
                {:else if lostMissingItems.length === 0}
                    <p class="muted">No missing items.</p>
                {:else}
                    <div class="grid">
                        {#each lostMissingItems as loan}
                            <article class="card loan">
                                <div class="loan-head">
                                    {#if loan.photo_filename}
                                        <img class="thumb" src="/api/photos/{loan.photo_filename}" alt="" on:error={(e) => (e.target.style.display = 'none')} />
                                    {/if}
                                    <h3>{loan.item_name}</h3>
                                    <span class="chip warn">Missing</span>
                                </div>
                                <div class="rows">
                                    <div class="row"><span class="muted">Borrower</span><span>{loan.borrower_name}</span></div>
                                    <div class="row"><span class="muted">Phone</span><button class="phone mono" title="Copy phone number" on:click={() => copyToClipboard(loan.borrower_phone)}>{loan.borrower_phone}</button></div>
                                    <div class="row"><span class="muted">Lab</span><span>{loan.lab_location}</span></div>
                                    <div class="row"><span class="muted">Quantity</span><span class="mono">{loan.quantity_borrowed}</span></div>
                                    <div class="row"><span class="muted">Expected return</span><span class="mono">{formatExpectedReturn(loan.expected_return_date)}</span></div>
                                    {#if loan.approved_by}<div class="row"><span class="muted">Marked missing by</span><span>{loan.approved_by}</span></div>{/if}
                                    <div class="row"><span class="muted">Borrowed</span><span class="mono">{formatDate(loan.CreatedAt)}</span></div>
                                </div>
                                {#if loan.status === 'not_found'}
                                    <div class="actions">
                                        <button class="btn" on:click={() => markAsFound(loan.ID)} disabled={loading}>Mark as found</button>
                                    </div>
                                {/if}
                            </article>
                        {/each}
                    </div>
                {/if}
            </section>
        {/if}

        <!-- 3D printers -->
        {#if currentView === 'printers'}
            <section>
                <div class="view-head">
                    <h2>3D printers</h2>
                    <a class="btn" href="/printers" target="_blank" rel="noopener">Open full page</a>
                </div>
                <p class="intro muted">Live status, refreshing every few seconds.</p>

                {#if printers.length === 0}
                    <p class="muted">No printers are configured.</p>
                {:else}
                    <div class="grid printers">
                        {#each printers as printer (printer.id)}
                            {@const st = printerStateClass(printer)}
                            <article class="card printer">
                                <div class="printer-head">
                                    <img src="/P1S.png" alt="" class="printer-img" />
                                    <div class="grow">
                                        <h3>{printer.name}</h3>
                                        <p class="muted small">{printer.online ? (printerIsPrinting(printer) ? 'In use' : 'Free') : 'Unknown'}</p>
                                    </div>
                                    <span class="chip" class:hue={PRINTER_HUE[st]} style="--hue: {PRINTER_HUE[st] || 'var(--muted)'}">{printerStateLabel(printer)}</span>
                                </div>

                                <div class="camera">
                                    {#if printer.camera_online && $camera[printer.id]}
                                        <img src={$camera[printer.id]} alt="Camera view of {printer.name}" />
                                    {:else}
                                        <p class="muted small">{printer.camera_online ? 'Loading camera…' : 'No camera image'}</p>
                                    {/if}
                                </div>

                                {#if printer.faults && printer.faults.length > 0}
                                    <div class="notice">
                                        <p><strong>Printer reported {printer.faults.length} fault{printer.faults.length > 1 ? 's' : ''}</strong></p>
                                        {#each printer.faults as fault}
                                            <a class="fault" href={fault.url} target="_blank" rel="noopener">
                                                <span class="chip" class:warn={fault.severity === 'fatal' || fault.severity === 'serious'}>{fault.severity}</span>
                                                <span class="mono">{fault.code}</span>
                                                <span class="muted small">Look up</span>
                                            </a>
                                        {/each}
                                    </div>
                                {/if}

                                {#if printer.access_code_problem}
                                    <div class="notice">
                                        <p><strong>Access code changed</strong></p>
                                        <p class="muted small">{printer.name} is refusing our access code. LAN mode was probably toggled.</p>
                                        {#if editingCode === printer.id}
                                            <div class="actions">
                                                <input type="text" bind:value={newCode} placeholder="New access code" autocomplete="off" />
                                                <button class="btn primary" on:click={() => savePrinterCode(printer)} disabled={savingCode}>{savingCode ? 'Saving…' : 'Save'}</button>
                                                <button class="btn" on:click={() => (editingCode = '')}>Cancel</button>
                                            </div>
                                        {:else}
                                            <div class="actions">
                                                <button class="btn" on:click={() => { editingCode = printer.id; newCode = ''; }}>Update access code</button>
                                            </div>
                                        {/if}
                                    </div>
                                {/if}

                                {#if printer.online}
                                    {#if printerIsPrinting(printer)}
                                        <div class="progress" style="--hue: {PRINTER_HUE.running}">
                                            <div class="bar"><span style="width: {printer.progress}%"></span></div>
                                            <span class="mono small">{printer.progress}%</span>
                                        </div>
                                        {#if printer.remaining_minutes > 0}
                                            <p class="muted small mono">{formatRemaining(printer.remaining_minutes)}</p>
                                        {/if}
                                    {/if}

                                    {#if printer.file_name}
                                        <p class="file small" title={printer.file_name}>{printer.file_name}</p>
                                    {/if}

                                    {#if (printer.ams && printer.ams.length > 0) || printer.external_spool}
                                        <div class="ams">
                                            {#each printer.ams as unit}
                                                <div class="ams-head small">
                                                    <span>AMS {unit.id + 1}</span>
                                                    {#if unit.humidity}
                                                        <span class="muted mono" title="The AMS reports humidity on its own 1-5 scale">Humidity {unit.humidity}/5</span>
                                                    {/if}
                                                    {#if unit.temp}<span class="muted mono">{unit.temp}°C</span>{/if}
                                                </div>
                                                <div class="slots">
                                                    {#each unit.slots as slot}
                                                        <div
                                                            class="slot"
                                                            class:empty={slot.empty}
                                                            class:active={slot.active}
                                                            title={slot.empty
                                                                ? `Slot ${slot.slot}: empty`
                                                                : `Slot ${slot.slot}: ${slot.material}${slot.remain >= 0 ? ', ' + slot.remain + '% left' : ', amount unknown'}${slot.active ? ' (in use)' : ''}`}
                                                        >
                                                            <span class="slot-top">
                                                                <span class="swatch" style="background: {slot.color || 'transparent'}"></span>
                                                                <span class="mono muted">{slot.slot}</span>
                                                            </span>
                                                            <span class="slot-mat">{slot.empty ? 'Empty' : slot.material}</span>
                                                            {#if slot.remain >= 0}
                                                                <div class="bar"><span style="width: {slot.remain}%"></span></div>
                                                                <span class="mono muted">{slot.remain}%</span>
                                                            {:else if !slot.empty}
                                                                <span class="muted" title="No RFID tag, so the printer cannot tell">? left</span>
                                                            {/if}
                                                        </div>
                                                    {/each}
                                                </div>
                                            {/each}

                                            {#if printer.external_spool && !printer.external_spool.empty}
                                                <div class="ams-head small">
                                                    <span class="swatch" style="background: {printer.external_spool.color || 'transparent'}"></span>
                                                    <span>External spool</span>
                                                    <span class="muted">{printer.external_spool.material}</span>
                                                    {#if printer.external_spool.remain >= 0}
                                                        <span class="mono muted">{printer.external_spool.remain}%</span>
                                                    {/if}
                                                </div>
                                            {/if}
                                        </div>
                                    {/if}

                                    <div class="temps mono muted small">
                                        <span>Nozzle {printer.nozzle_temp.toFixed(0)}°C</span>
                                        <span>Bed {printer.bed_temp.toFixed(0)}°C</span>
                                        {#if printer.chamber_temp > 0}<span>Chamber {printer.chamber_temp.toFixed(0)}°C</span>{/if}
                                    </div>

                                    <div class="actions">
                                        <button
                                            class="btn"
                                            aria-pressed={printer.light_on}
                                            on:click={() => toggleLight(printer)}
                                            disabled={busyPrinter === printer.id + 'light'}
                                            title="Chamber light: the camera is useless in the dark"
                                        >{printer.light_on ? 'Light on' : 'Light off'}</button>
                                        {#if printerIsPrinting(printer)}
                                            <button class="btn" on:click={() => pausePrint(printer)} disabled={busyPrinter === printer.id + 'pause'}>Pause</button>
                                        {:else if printer.state === 'PAUSE'}
                                            <button class="btn" on:click={() => resumePrint(printer)} disabled={busyPrinter === printer.id + 'resume'}>Resume</button>
                                        {/if}
                                        {#if printerIsPrinting(printer) || printer.state === 'PAUSE'}
                                            <button class="btn danger" on:click={() => stopPrint(printer)} disabled={stoppingPrinter === printer.id}>
                                                {stoppingPrinter === printer.id ? 'Stopping…' : 'Stop print'}
                                            </button>
                                        {/if}
                                        <button class="btn" on:click={() => toggleUpload(printer)}>{uploadFor === printer.id ? 'Close files' : 'Send a file'}</button>
                                        {#if canStart(printer)}
                                            <button class="btn" on:click={() => toggleStart(printer)}>{startFor === printer.id ? 'Close' : 'Start a print'}</button>
                                        {/if}
                                    </div>

                                    {#if uploadFor === printer.id}
                                        <div class="panel">
                                            <p class="muted small">
                                                File names should start with the owner's name, e.g.
                                                <code>srinath_bracket.3mf</code>. The file lands on the
                                                printer; start it from the screen once the plate is clear.
                                            </p>
                                            <label class="btn file-pick">
                                                <input type="file" accept=".3mf,.gcode" on:change={(e) => sendFile(printer, e)} disabled={uploading === printer.id} />
                                                <span>{uploading === printer.id ? `Sending… ${uploadProgress}%` : 'Choose a .3mf or .gcode file'}</span>
                                            </label>
                                            {#if uploading === printer.id}
                                                <div class="bar"><span style="width: {uploadProgress}%"></span></div>
                                            {/if}
                                            {#if uploadFiles[printer.id]}
                                                {#if uploadFiles[printer.id].length === 0}
                                                    <p class="muted small">No printable files on this printer.</p>
                                                {:else}
                                                    <div>
                                                        {#each uploadFiles[printer.id] as file}
                                                            <div class="row small">
                                                                <span class="file" title={file.name}>{file.name}</span>
                                                                <span class="mono muted">{fileSize(file.size)}</span>
                                                                <button class="btn danger sm" on:click={() => deletePrinterFile(printer, file.name)} title="Delete from the printer">Delete</button>
                                                            </div>
                                                        {/each}
                                                    </div>
                                                    <button class="btn danger" on:click={() => clearPrinterFiles(printer)} disabled={clearing === printer.id}>
                                                        {clearing === printer.id ? 'Clearing…' : `Clear all ${uploadFiles[printer.id].length} file(s)`}
                                                    </button>
                                                {/if}
                                            {/if}
                                        </div>
                                    {/if}

                                    {#if startFor === printer.id}
                                        <div class="panel">
                                            <label class="field">
                                                <span>File</span>
                                                <select bind:value={startFile}>
                                                    <option value="">Choose a file…</option>
                                                    {#each uploadFiles[printer.id] || [] as file}
                                                        <option value={file.name}>{file.name}</option>
                                                    {/each}
                                                </select>
                                            </label>
                                            <label class="field">
                                                <span>Plate fitted</span>
                                                <select bind:value={startBed}>
                                                    {#each BED_TYPES as bed}<option value={bed.value}>{bed.label}</option>{/each}
                                                </select>
                                            </label>
                                            <label class="field">
                                                <span>Filament</span>
                                                <select bind:value={startSpool}>
                                                    {#each amsChoices(printer) as choice}
                                                        <option value={String(choice.index)}>
                                                            AMS {choice.unit} slot {choice.slot}: {choice.label}{choice.remain >= 0 ? ` (${choice.remain}%)` : ''}
                                                        </option>
                                                    {/each}
                                                    <option value="external">External spool</option>
                                                </select>
                                            </label>
                                            {#if startSpool !== 'external'}
                                                <div class="swatches">
                                                    {#each amsChoices(printer) as choice}
                                                        <button
                                                            class="swatch-btn mono"
                                                            class:picked={startSpool === String(choice.index)}
                                                            style="background: {choice.color || 'var(--faint)'}"
                                                            title="AMS {choice.unit} slot {choice.slot}: {choice.label}"
                                                            on:click={() => (startSpool = String(choice.index))}
                                                        >{choice.slot}</button>
                                                    {/each}
                                                </div>
                                            {/if}
                                            <label class="field">
                                                <span>Plate number</span>
                                                <input type="number" min="1" max="64" bind:value={startPlate} />
                                            </label>
                                            <div class="checks">
                                                <label class="check"><input type="checkbox" bind:checked={startLevel} /> Bed levelling</label>
                                                <label class="check"><input type="checkbox" bind:checked={startFlow} /> Flow calibration</label>
                                                <label class="check"><input type="checkbox" bind:checked={startTimelapse} /> Timelapse</label>
                                            </div>
                                            <p class="muted small">Check the camera above: the plate must be clear and the plate type must match what you chose.</p>
                                            <button class="btn primary" on:click={() => startPrint(printer)} disabled={starting === printer.id || !startFile}>
                                                {starting === printer.id ? 'Starting…' : 'Start print'}
                                            </button>
                                        </div>
                                    {/if}

                                    {#if printer.last_action_by}
                                        <p class="muted small">Last stopped by {printer.last_action_by}</p>
                                    {/if}
                                {:else}
                                    <p class="muted small">Not responding: switched off or off the network.</p>
                                {/if}
                            </article>
                        {/each}
                    </div>
                {/if}

                <div class="view-head sub">
                    <h3>Print log</h3>
                    <!-- A plain link would go without the admin token and get a 401 -->
                    <button type="button" class="btn" on:click={() => exportCSV('/api/admin/export-print-jobs-csv', 'print_jobs.csv')}>Export CSV</button>
                </div>
                <p class="intro muted">Recorded automatically from the printers. Whose print it is comes from the file name.</p>

                {#if printJobs.length === 0}
                    <p class="muted">No prints recorded yet.</p>
                {:else}
                    <div class="table-wrap">
                        <table>
                            <thead><tr><th>Started</th><th>Printer</th><th>File</th><th>Duration</th><th>Result</th></tr></thead>
                            <tbody>
                                {#each printJobs.slice(0, 40) as job}
                                    <tr>
                                        <td class="mono nowrap">
                                            {new Date(job.started_at).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}
                                            {new Date(job.started_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                        </td>
                                        <td>{job.printer_name}</td>
                                        <td class="file" title={job.file_name}>{job.file_name || '(unnamed)'}</td>
                                        <td class="mono">{jobDuration(job)}</td>
                                        <td><span class="chip" class:ok={job.result === 'finished'} class:hue={job.result === 'failed'} style="--hue: var(--danger)">{job.result}{#if job.stopped_by} by {job.stopped_by}{/if}</span></td>
                                    </tr>
                                {/each}
                            </tbody>
                        </table>
                    </div>
                {/if}
            </section>
        {/if}

        <!-- Bookings -->
        {#if currentView === 'bookings'}
            <section>
                <div class="view-head">
                    <h2>Bookings</h2>
                    <div class="seg" role="tablist">
                        <button role="tab" aria-selected={bookingLab === 'mocap'} class:active={bookingLab === 'mocap'} on:click={() => (bookingLab = 'mocap')}><span class="dot" style="--hue: {AREA.mocap}"></span>Mocap Lab</button>
                        <button role="tab" aria-selected={bookingLab === 'mech'} class:active={bookingLab === 'mech'} on:click={() => (bookingLab = 'mech')}><span class="dot" style="--hue: {AREA.mech}"></span>Mech Lab</button>
                    </div>
                </div>
                {#key `${bookingLab}-${calendarVersion}`}
                    <BookingPage lab={bookingLab} title={bookingLab === 'mocap' ? 'Motion Capture Lab' : 'Mech Lab robots'} embedded />
                {/key}

                <div class="view-head sub">
                    <h3>Robots and labs</h3>
                    <button class="btn" on:click={() => editResource(null)}>Add robot</button>
                </div>
                <p class="intro muted">Each robot has its own calendar. Research threads get access from a lab admin on the Threads page; "needs approval" also sends every booking to a lab admin.</p>
                <div class="table-wrap">
                    <table>
                        <thead><tr><th>Name</th><th>Id</th><th>Lab</th><th>Rules</th><th></th></tr></thead>
                        <tbody>
                            {#each resources as entry (entry.resource.ID)}
                                <tr class:dim={!entry.resource.active}>
                                    <td><span class="with-dot"><span class="dot" style="--hue: {hueFor(entry.resource.ID)}"></span>{entry.resource.name}</span></td>
                                    <td class="mono">{entry.resource.slug}</td>
                                    <td><span class="with-dot"><span class="dot" style="--hue: {AREA[entry.resource.lab] || 'var(--muted)'}"></span>{entry.resource.lab}</span></td>
                                    <td>
                                        {[entry.resource.requires_thread && 'threads', entry.resource.requires_approval && 'approval',
                                          entry.resource.max_hours && `≤${entry.resource.max_hours}h`, entry.resource.max_future_bookings && `≤${entry.resource.max_future_bookings} upcoming`]
                                          .filter(Boolean).join(', ') || '—'}
                                    </td>
                                    <td class="end"><button class="btn sm" on:click={() => editResource(entry)}>Edit</button></td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>

                {#if editingResource}
                    <form class="card form hued" style="--hue: {AREA.mech}" on:submit|preventDefault={saveResource}>
                        <p class="label">{editingResource.ID ? 'Edit robot' : 'New robot'}</p>
                        <label class="field"><span>Name</span><input type="text" bind:value={editingResource.name} required placeholder="UR5 #1" /></label>
                        {#if !editingResource.ID}
                            <label class="field"><span>Id (can't change later)</span><input type="text" bind:value={editingResource.slug} required pattern="[a-z0-9][a-z0-9-]*" placeholder="ur5-1" /></label>
                        {/if}
                        <label class="field">
                            <span>Lab</span>
                            <select bind:value={editingResource.lab}>
                                <option value="mech">Mech Lab</option>
                                <option value="mocap">Mocap Lab</option>
                            </select>
                        </label>
                        <label class="check"><input type="checkbox" bind:checked={editingResource.requires_thread} /> Only research threads with access can book it</label>
                        <label class="check"><input type="checkbox" bind:checked={editingResource.requires_approval} /> Every booking needs a lab admin's approval</label>
                        <label class="check"><input type="checkbox" bind:checked={editingResource.active} /> Bookable</label>
                        <label class="field"><span>Longest booking, hours (0 = 12)</span><input type="number" min="0" max="168" bind:value={editingResource.max_hours} /></label>
                        <label class="field"><span>Upcoming bookings per person (0 = no limit)</span><input type="number" min="0" bind:value={editingResource.max_future_bookings} /></label>
                        <div class="actions">
                            <button type="submit" class="btn primary">Save</button>
                            <button type="button" class="btn" on:click={() => (editingResource = null)}>Cancel</button>
                        </div>
                    </form>
                {/if}

                <div class="view-head sub"><h3>All bookings</h3></div>
                {#if loading}
                    <p class="muted">Loading…</p>
                {:else if bookings.length === 0}
                    <p class="muted">No bookings.</p>
                {:else}
                    <div class="table-wrap">
                        <table>
                            <thead><tr><th>When</th><th>Resource</th><th>Purpose</th><th>Booked by</th><th>Phone</th><th></th></tr></thead>
                            <tbody>
                                {#each bookings as booking}
                                    <tr class:dim={new Date(booking.end_time) < new Date()}>
                                        <td class="mono nowrap">
                                            {new Date(booking.start_time).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}
                                            {new Date(booking.start_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}–{new Date(booking.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                        </td>
                                        <td>
                                            <span class="with-dot"><span class="dot" style="--hue: {hueFor(booking.resource_id)}"></span>{resourceName[booking.resource_id] || 'Unknown resource'}</span>
                                            {#if booking.thread_name}
                                                <span class="with-dot muted">·{#if booking.thread_id}<span class="dot" style="--hue: {hueFor(booking.thread_id)}"></span>{/if}{booking.thread_name}</span>
                                            {/if}
                                        </td>
                                        <td>{booking.purpose}</td>
                                        <td>{booking.booked_by}</td>
                                        <td><button class="phone mono" title="Copy phone number" on:click={() => copyToClipboard(booking.phone)}>{booking.phone}</button></td>
                                        <td class="end"><button class="btn danger sm" on:click={() => deleteBooking(booking)}>Delete</button></td>
                                    </tr>
                                {/each}
                            </tbody>
                        </table>
                    </div>
                {/if}
            </section>
        {/if}

        <!-- Items (catalogue upkeep) -->
        {#if currentView === 'items'}
            <section>
                <div class="view-head">
                    <h2>Items</h2>
                    <button class="btn" on:click={loadCatalogue}>Refresh</button>
                </div>
                <p class="intro muted">People add items as they borrow. Here you tidy up: merge duplicates, mark valuable items as tracked (their custodian confirms them monthly), set rules.</p>
                <input class="search" type="search" bind:value={itemSearch} placeholder="Search items by name" aria-label="Search items by name" />

                {#if editingItem}
                    <form class="card form hued" style="--hue: {AREA.equipment}" on:submit|preventDefault={saveItem}>
                        <p class="label">Edit item</p>
                        <label class="field"><span>Name</span><input type="text" bind:value={editingItem.name} required /></label>
                        <label class="field">
                            <span>Lab</span>
                            <select bind:value={editingItem.home_lab}>
                                {#each labs as lab}<option value={lab}>{lab}</option>{/each}
                                {#if editingItem.home_lab && !labs.includes(editingItem.home_lab)}
                                    <option value={editingItem.home_lab}>{editingItem.home_lab}</option>
                                {/if}
                            </select>
                        </label>
                        <label class="field"><span>Total quantity (0 = unknown)</span><input type="number" min="0" bind:value={editingItem.total_quantity} /></label>
                        <label class="field"><span>Notes</span><input type="text" maxlength="500" bind:value={editingItem.notes} /></label>
                        <label class="check"><input type="checkbox" bind:checked={editingItem.tracked} /> Tracked (custodian confirms it monthly)</label>
                        <label class="check"><input type="checkbox" bind:checked={editingItem.lab_only} /> Lab only (can't leave the lab)</label>
                        <label class="check"><input type="checkbox" bind:checked={editingItem.needs_approval} /> Borrowing needs approval</label>
                        {#if editingItem.tracked}
                            <label class="field">
                                <span>Custodian</span>
                                <select bind:value={editingItem.custodian_id}>
                                    <option value={null}>None</option>
                                    {#each people as person}<option value={person.id}>{person.name}</option>{/each}
                                </select>
                            </label>
                        {/if}
                        <div class="actions">
                            <button type="submit" class="btn primary">Save</button>
                            <button type="button" class="btn" on:click={() => (editingItem = null)}>Cancel</button>
                        </div>
                    </form>
                {/if}

                <div class="table-wrap">
                    <table>
                        <thead><tr><th>Name</th><th>Lab</th><th>Out</th><th>Rules</th><th>Issue</th><th></th></tr></thead>
                        <tbody>
                            {#each shownItems as item (item.ID)}
                                <tr>
                                    <td>{item.name}</td>
                                    <td><span class="with-dot">{#if LAB[item.home_lab]}<span class="dot" style="--hue: {LAB[item.home_lab]}"></span>{/if}{item.home_lab || '—'}</span></td>
                                    <td class="mono">{itemsOut(item)}</td>
                                    <td class="chips">
                                        {#if item.tracked}<span class="chip">tracked</span>{/if}
                                        {#if item.lab_only}<span class="chip">lab only</span>{/if}
                                        {#if item.needs_approval}<span class="chip">needs approval</span>{/if}
                                        {#if item.total_quantity}<span class="chip mono">×{item.total_quantity}</span>{/if}
                                    </td>
                                    <td>
                                        {#if item.issue}
                                            <span class="issue">{item.issue}</span>{#if item.issue_reported_by}<span class="muted"> ({item.issue_reported_by})</span>{/if}
                                            <button class="btn sm" on:click={() => clearIssue(item)}>Clear</button>
                                        {:else}—{/if}
                                    </td>
                                    <td class="end">
                                        <div class="actions">
                                            <button class="btn sm" on:click={() => (editingItem = { ...item, notes: item.notes || '', custodian_id: item.custodian_id ?? null })}>Edit</button>
                                            <select class="sm" bind:value={mergeInto[item.ID]} aria-label="Merge {item.name} into">
                                                <option value={undefined}>Merge into…</option>
                                                {#each catalogue as other (other.ID)}
                                                    {#if other.ID !== item.ID}<option value={other.ID}>{other.name}</option>{/if}
                                                {/each}
                                            </select>
                                            <button class="btn sm" disabled={!mergeInto[item.ID]} on:click={() => mergeItem(item)}>Merge</button>
                                            <button class="btn danger sm" on:click={() => deleteItem(item)}>Delete</button>
                                        </div>
                                    </td>
                                </tr>
                            {:else}
                                <tr><td colspan="6" class="muted">No items{itemSearch.trim() ? ` match "${itemSearch}"` : ''}.</td></tr>
                            {/each}
                        </tbody>
                    </table>
                </div>
            </section>
        {/if}

        <!-- Item history -->
        {#if currentView === 'history'}
            <section>
                <div class="view-head"><h2>Item history</h2></div>
                <p class="intro muted">Every loan, newest first: borrowed, returned and missing.</p>

                <div class="actions search-row">
                    <input
                        class="search"
                        type="search"
                        id="history-search"
                        bind:value={historySearchQuery}
                        placeholder="Search name, item, phone, lab, purpose, status or admin"
                        aria-label="Search history"
                        autocomplete="off"
                    />
                    {#if historySearchQuery && historySearchQuery.trim()}
                        <button class="btn" type="button" on:click={clearHistorySearch}>Clear</button>
                    {/if}
                </div>
                {#if historySearchQuery.trim()}
                    <p class="muted small">Showing <span class="mono">{filteredHistoryItems.length}</span> of <span class="mono">{historyItems.length}</span> records</p>
                {/if}

                {#if loading}
                    <p class="muted">Loading…</p>
                {:else if historyItems.length === 0}
                    <p class="muted">No history found.</p>
                {:else if filteredHistoryItems.length === 0 && historySearchQuery.trim()}
                    <p class="muted">No records match "{historySearchQuery}".</p>
                {:else}
                    <div class="table-wrap">
                        <table>
                            <thead>
                                <tr><th>Item</th><th>Status</th><th>Borrower</th><th>Phone</th><th>Lab</th><th>Qty</th><th>Purpose</th><th>Borrowed</th><th>Expected return</th><th>Closed</th><th>Handled by</th></tr>
                            </thead>
                            <tbody>
                                {#each filteredHistoryItems as item}
                                    {@const late = isOverdue(item.expected_return_date)}
                                    <tr>
                                        <td>
                                            <span class="item-cell">
                                                {#if item.photo_filename}
                                                    <img class="thumb sm" src="/api/photos/{item.photo_filename}" alt="" on:error={(e) => (e.target.style.display = 'none')} />
                                                {/if}
                                                {item.item_name}
                                            </span>
                                        </td>
                                        <td class="chips">
                                            {#if item.status === 'returned'}
                                                <span class="chip">Returned</span>
                                                {#if late}<span class="chip warn">Late</span>{/if}
                                            {:else if item.status === 'not_found'}
                                                <span class="chip warn">Missing</span>
                                            {:else if item.status === 'active'}
                                                <span class="chip ok">Borrowed</span>
                                                {#if late}<span class="chip warn">Overdue</span>{/if}
                                            {:else}
                                                <span class="chip">{item.status.charAt(0).toUpperCase() + item.status.slice(1)}</span>
                                            {/if}
                                        </td>
                                        <td>{item.borrower_name}</td>
                                        <td><button class="phone mono" title="Copy phone number" on:click={() => copyToClipboard(item.borrower_phone)}>{item.borrower_phone}</button></td>
                                        <td><span class="with-dot">{#if LAB[item.lab_location]}<span class="dot" style="--hue: {LAB[item.lab_location]}"></span>{/if}{item.lab_location}</span></td>
                                        <td class="mono">{item.quantity_borrowed}</td>
                                        <td>{item.purpose}</td>
                                        <td class="mono nowrap">{formatDate(item.CreatedAt)}</td>
                                        <td class="mono nowrap" class:overdue={item.status === 'active' && late}>{item.expected_return_date ? formatExpectedReturn(item.expected_return_date) : '—'}</td>
                                        <td class="mono nowrap">
                                            {#if item.status === 'returned' && item.returned_at}{formatDate(item.returned_at)}
                                            {:else if item.status === 'not_found' && item.approved_at}{formatDate(item.approved_at)}
                                            {:else}—{/if}
                                        </td>
                                        <td>{item.approved_by || '—'}</td>
                                    </tr>
                                {/each}
                            </tbody>
                        </table>
                    </div>
                {/if}
            </section>
        {/if}

        <!-- Lab view -->
        {#if currentView === 'lab-view'}
            <section>
                <div class="view-head">
                    <h2 class="with-dot"><span class="dot" style="--hue: {LAB[selectedLab]}"></span>{selectedLab}</h2>
                    <div class="seg">
                        <button class:active={selectedFilter === 'all'} on:click={() => filterLabLoans('all')}>All</button>
                        <button class:active={selectedFilter === 'borrowed'} on:click={() => filterLabLoans('borrowed')}>Borrowed</button>
                        <button class:active={selectedFilter === 'returned'} on:click={() => filterLabLoans('returned')}>Returned</button>
                        <button class:active={selectedFilter === 'not_found'} on:click={() => filterLabLoans('not_found')}>Missing</button>
                    </div>
                </div>
                <p class="intro muted">Borrowed items. Returns older than two weeks move to Item history.</p>

                {#if loading}
                    <p class="muted">Loading…</p>
                {:else if labLoans.length === 0}
                    <p class="muted">No items for this filter.</p>
                {:else}
                    <div class="grid">
                        {#each labLoans as loan}
                            {@const overdue = isOverdue(loan.expected_return_date) && loan.status === 'active'}
                            <article class="card loan">
                                <div class="loan-head">
                                    {#if loan.photo_filename}
                                        <img class="thumb" src="/api/photos/{loan.photo_filename}" alt="" on:error={(e) => (e.target.style.display = 'none')} />
                                    {/if}
                                    <h3>{loan.item_name}</h3>
                                    {#if loan.status === 'not_found'}
                                        <span class="chip warn">Missing</span>
                                    {:else if loan.status === 'returned'}
                                        <span class="chip">Returned</span>
                                    {:else if overdue}
                                        <span class="chip warn">Overdue</span>
                                    {:else}
                                        <span class="chip ok">Borrowed</span>
                                    {/if}
                                </div>
                                <div class="rows">
                                    <div class="row"><span class="muted">Borrower</span><span>{loan.borrower_name}</span></div>
                                    <div class="row"><span class="muted">Phone</span><button class="phone mono" title="Copy phone number" on:click={() => copyToClipboard(loan.borrower_phone)}>{loan.borrower_phone}</button></div>
                                    <div class="row"><span class="muted">Quantity</span><span class="mono">{loan.quantity_borrowed}</span></div>
                                    <div class="row"><span class="muted">Purpose</span><span>{loan.purpose}</span></div>
                                    <div class="row"><span class="muted">Expected return</span><span class="mono" class:overdue>{formatExpectedReturn(loan.expected_return_date)}</span></div>
                                    <div class="row"><span class="muted">Borrowed</span><span class="mono">{formatDate(loan.CreatedAt)}</span></div>
                                    {#if loan.status === 'returned'}
                                        <div class="row">
                                            <span class="muted">Returned</span>
                                            <span class="mono">{loan.returned_at ? formatDate(loan.returned_at) : '—'}{#if isOverdue(loan.expected_return_date)} <span class="chip warn">Late</span>{/if}</span>
                                        </div>
                                    {/if}
                                    {#if loan.approved_by}<div class="row"><span class="muted">Last handled by</span><span>{loan.approved_by}</span></div>{/if}
                                </div>
                                {#if loan.status === 'active'}
                                    <div class="extend small">
                                        <span class="muted">Extend by</span>
                                        <input type="number" id="extend-days-{loan.ID}" aria-label="Extend by days" min="0" max="365" placeholder="Days" value="0" />
                                        <span class="muted">days</span>
                                        <input type="number" id="extend-hours-{loan.ID}" aria-label="Extend by hours" min="0" max="23" placeholder="Hours" value="0" />
                                        <span class="muted">hours</span>
                                    </div>
                                    <div class="actions">
                                        <button
                                            class="btn"
                                            on:click={() => {
                                                const days = document.getElementById(`extend-days-${loan.ID}`).value;
                                                const hours = document.getElementById(`extend-hours-${loan.ID}`).value;
                                                extendLoan(loan.ID, days, hours);
                                            }}
                                            disabled={loading}
                                        >Extend</button>
                                        <button class="btn danger" on:click={() => markAsMissing(loan.ID, loan.item_name)} disabled={loading}>Can't find it</button>
                                    </div>
                                {:else if loan.status === 'not_found'}
                                    <div class="actions">
                                        <button class="btn" on:click={() => markAsFound(loan.ID)} disabled={loading}>Mark as found</button>
                                    </div>
                                {/if}
                            </article>
                        {/each}
                    </div>
                {/if}
            </section>
        {/if}

        <!-- Users and groups (server admins) -->
        {#if currentView === 'users' && can(adminInfo, 'manage_users')}
            <section>
                <div class="view-head">
                    <h2>Users</h2>
                    <button class="btn" on:click={loadUsers}>Refresh</button>
                </div>

                {#if pendingUsers.length > 0}
                    <div class="card block hued" style="--hue: var(--warn)">
                        <p class="label">Waiting for approval <span class="mono">{pendingUsers.length}</span></p>
                        {#each pendingUsers as user (user.id)}
                            <div class="row wrap">
                                <div>
                                    <strong>{user.name}</strong>
                                    <p class="muted small">{user.email} · <span class="mono">{user.phone}</span></p>
                                    <p class="muted small">Signed up <span class="mono">{new Date(user.created_at).toLocaleDateString()}</span></p>
                                </div>
                                <div class="actions">
                                    <span class="dot" style="--hue: {hueFor(user.group_id)}"></span>
                                    <select value={user.group_id} on:change={(e) => changeGroup(e, user)} aria-label="Group for {user.name}">
                                        {#each groups as g}<option value={g.id}>{g.name}</option>{/each}
                                    </select>
                                    <button class="btn primary" on:click={() => updateUser(user, { status: 'active' }, `${user.name} approved`)}>Approve</button>
                                    <button class="btn danger" on:click={() => updateUser(user, { status: 'disabled' }, `${user.name} rejected`)}>Reject</button>
                                </div>
                            </div>
                        {/each}
                    </div>
                {/if}

                <div class="view-head sub"><h3>Everyone</h3></div>
                <div class="table-wrap">
                    <table>
                        <thead><tr><th>Name</th><th>Email</th><th>Group</th><th>Status</th><th>Extra access</th><th></th></tr></thead>
                        <tbody>
                            {#each users.filter((u) => u.status !== 'pending') as user (user.id)}
                                <tr class:dim={user.status === 'disabled'}>
                                    <td>{user.name}</td>
                                    <td>{user.email}</td>
                                    <td>
                                        {#if user.id === adminInfo.id}
                                            <span class="chip hue" style="--hue: {hueFor(user.group_id)}">{groupName(user.group_id)}</span> <span class="muted">(you)</span>
                                        {:else}
                                            <span class="dot" style="--hue: {hueFor(user.group_id)}"></span>
                                            <select class="sm" value={user.group_id} on:change={(e) => changeGroup(e, user)} aria-label="Group for {user.name}">
                                                {#each groups as g}<option value={g.id}>{g.name}</option>{/each}
                                            </select>
                                        {/if}
                                    </td>
                                    <td><span class="chip" class:ok={user.status === 'active'} class:warn={user.status === 'pending'}>{user.status}</span></td>
                                    <td class="chips">
                                        {#each user.overrides as o}
                                            <span class="chip" class:ok={o.allow} class:warn={!o.allow}>{o.allow ? '+' : '−'} {permLabel(o.perm)}</span>
                                        {/each}
                                    </td>
                                    <td class="end">
                                        {#if user.id !== adminInfo.id}
                                            <div class="actions">
                                                <button class="btn sm" on:click={() => editOverrides(user)}>Access</button>
                                                <button class="btn sm" on:click={() => resetPassword(user)}>Reset password</button>
                                                {#if user.status === 'active'}
                                                    <button class="btn danger sm" on:click={() => confirm(`Disable ${user.name}? They will be logged out.`) && updateUser(user, { status: 'disabled' }, `${user.name} disabled`)}>Disable</button>
                                                {:else}
                                                    <button class="btn sm" on:click={() => updateUser(user, { status: 'active' }, `${user.name} enabled`)}>Enable</button>
                                                {/if}
                                            </div>
                                        {/if}
                                    </td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>

                {#if editingUser}
                    <div class="card form hued" style="--hue: {AREA.admin}">
                        <p class="label">Access for {editingUser.name} ({groupName(editingUser.group_id)})</p>
                        <p class="muted small">"Group default" follows the group's settings below. Grant or revoke to make an exception for this person.</p>
                        <div>
                            {#each draftPerms as perm}
                                <label class="row">
                                    <span>{permLabel(perm)}</span>
                                    <select bind:value={overrideDraft[perm]}>
                                        <option value={undefined}>Group default</option>
                                        <option value="grant">Grant</option>
                                        <option value="revoke">Revoke</option>
                                    </select>
                                </label>
                            {/each}
                        </div>
                        <div class="actions">
                            <button class="btn primary" on:click={saveOverrides}>Save</button>
                            <button class="btn" on:click={() => (editingUser = null)}>Cancel</button>
                        </div>
                    </div>
                {/if}

                <div class="view-head sub"><h3>Groups</h3></div>
                <p class="intro muted">What each group can do by default. Changes apply immediately to everyone in the group.</p>
                <div class="table-wrap">
                    <table>
                        <thead><tr><th></th>{#each groups as g}<th class="center"><span class="with-dot"><span class="dot" style="--hue: {hueFor(g.id)}"></span>{g.name}</span></th>{/each}</tr></thead>
                        <tbody>
                            {#each allPermissions as perm}
                                <tr>
                                    <td>{permLabel(perm)}</td>
                                    {#each groups as g}
                                        <td class="center">
                                            <input type="checkbox" checked={g.permissions.includes(perm)}
                                                on:change={(e) => toggleGroupPerm(e, g, perm)}
                                                aria-label="{g.name}: {permLabel(perm)}" />
                                        </td>
                                    {/each}
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>

                <div class="view-head sub"><h3>Danger zone</h3></div>
                <div class="card hued" style="--hue: var(--danger)">
                    <div class="row wrap">
                        <div>
                            <strong>Delete all item data</strong>
                            <p class="muted small">Permanently deletes every inventory item. This cannot be undone.</p>
                        </div>
                        <button class="btn danger" on:click={deleteAllItems}>Delete all items</button>
                    </div>
                </div>
            </section>
        {/if}
    {/if}

    <footer class="credits muted small">
        Created by Srinath (<a href="https://github.com/Srindot" target="_blank" rel="noopener">GitHub</a>) · Theme:
        <a href="https://github.com/catppuccin/catppuccin" target="_blank" rel="noopener">Catppuccin</a>
    </footer>
</div>

<style>
    /* Tabs: plain text with an underline on the open one */
    .tabs {
        display: flex;
        gap: 4px;
        margin: -8px 0 24px;
        border-bottom: 1px solid var(--line);
        overflow-x: auto;
        scrollbar-width: none;
    }

    .tabs button {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        flex: none;
        padding: 12px 12px 10px;
        border: none;
        border-bottom: 2px solid transparent;
        margin-bottom: -1px;
        background: none;
        color: var(--muted);
        font: inherit;
        font-weight: 500;
        white-space: nowrap;
        cursor: pointer;
    }

    .tabs button:hover {
        color: var(--ctp-text);
    }

    .tabs button.active {
        color: var(--ctp-text);
        border-bottom-color: var(--hue, var(--accent));
    }

    .count {
        padding: 1px 7px;
        border-radius: var(--radius-pill);
        background: color-mix(in srgb, var(--hue, var(--accent)) 16%, transparent);
        color: var(--hue, var(--accent));
        font-size: 0.75rem;
    }

    .head-title {
        display: flex;
        align-items: center;
        gap: 14px;
    }

    .with-dot {
        display: inline-flex;
        align-items: center;
        gap: 6px;
    }

    td > .dot {
        margin-right: 6px;
        vertical-align: middle;
    }

    .seg .dot {
        margin-right: 6px;
        vertical-align: middle;
    }

    /* Overview cards are buttons into their tab */
    .link-card {
        display: block;
        width: 100%;
        text-align: left;
        font: inherit;
        color: inherit;
        cursor: pointer;
    }

    .link-card:hover {
        border-color: var(--line-strong);
    }

    .link-card.hued:hover {
        border-color: color-mix(in srgb, var(--hue) 55%, var(--line));
    }

    .link-card.filled:hover {
        filter: brightness(1.05);
    }

    .link-card .icon-badge {
        margin-bottom: 14px;
    }

    /* On a solid card the badge would vanish into its own hue */
    .filled .icon-badge {
        background: color-mix(in srgb, var(--accent-ink) 14%, transparent);
        color: var(--accent-ink);
    }

    .card-note {
        margin: 8px 0 0;
        color: var(--muted);
        font-size: 0.9rem;
    }

    .filled .card-note {
        color: inherit;
        opacity: 0.8;
    }

    /* Sections */
    section h2 {
        margin: 0;
        font-size: 1.15rem;
        font-weight: 600;
    }

    section h3 {
        margin: 0;
        font-size: 1rem;
        font-weight: 600;
    }

    .view-head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        flex-wrap: wrap;
        gap: 12px;
        margin-bottom: 6px;
    }

    .view-head.sub {
        margin-top: 32px;
    }

    .intro {
        margin: 0 0 16px;
        font-size: 0.9rem;
    }

    .small {
        font-size: 0.85rem;
    }

    p.small {
        margin: 4px 0 0;
    }

    .actions {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 8px;
    }

    .export {
        margin-top: 20px;
    }

    .seg {
        display: inline-flex;
        padding: 3px;
        gap: 2px;
        border: 1px solid var(--line);
        border-radius: var(--radius-sm);
        background: var(--card);
    }

    .seg button {
        padding: 6px 12px;
        border: none;
        border-radius: 6px;
        background: none;
        color: var(--muted);
        font: inherit;
        font-size: 0.9rem;
        cursor: pointer;
    }

    .seg button.active {
        background: var(--line-strong);
        color: var(--ctp-text);
    }

    /* Inputs */
    input:not([type='checkbox']):not([type='file']),
    select {
        box-sizing: border-box;
        min-height: 40px;
        padding: 8px 10px;
    }

    select.sm {
        min-height: 32px;
        padding: 4px 8px;
        font-size: 0.85rem;
    }

    input[type='checkbox'] {
        accent-color: var(--accent);
    }

    .search {
        width: 100%;
        max-width: 420px;
        margin-bottom: 16px;
    }

    .search-row .search {
        margin-bottom: 0;
    }

    .search-row {
        margin-bottom: 8px;
    }

    .btn.sm {
        min-height: 32px;
        padding: 0 10px;
        font-size: 0.85rem;
    }

    .phone {
        padding: 0;
        border: none;
        background: none;
        color: inherit;
        font-size: inherit;
        text-decoration: underline dotted var(--faint);
        text-underline-offset: 3px;
        cursor: copy;
    }

    .overdue {
        color: var(--danger);
    }

    /* Tables */
    .table-wrap {
        overflow-x: auto;
        background: var(--card);
        border: 1px solid var(--line);
        border-radius: var(--radius-lg);
    }

    table {
        width: 100%;
        border-collapse: collapse;
        font-size: 0.9rem;
    }

    th {
        padding: 10px 14px;
        text-align: left;
        font-size: 0.8rem;
        font-weight: 500;
        color: var(--muted);
        white-space: nowrap;
        border-bottom: 1px solid var(--line);
    }

    td {
        padding: 10px 14px;
        vertical-align: middle;
        border-top: 1px solid var(--line);
    }

    tbody tr:first-child td {
        border-top: none;
    }

    tr.dim td {
        color: var(--muted);
    }

    .center {
        text-align: center;
    }

    .end {
        text-align: right;
    }

    .end .actions {
        justify-content: flex-end;
        flex-wrap: nowrap;
    }

    .nowrap {
        white-space: nowrap;
    }

    .chips {
        white-space: nowrap;
    }

    .chips .chip + .chip {
        margin-left: 4px;
    }

    .issue {
        color: var(--warn);
    }

    td.file {
        max-width: 280px;
    }

    .file {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .item-cell {
        display: inline-flex;
        align-items: center;
        gap: 10px;
    }

    /* Forms */
    .form {
        display: grid;
        gap: 12px;
        max-width: 560px;
        margin: 16px 0;
    }

    .form .label {
        margin: 0;
    }

    .field {
        display: grid;
        gap: 6px;
        font-size: 0.85rem;
        color: var(--muted);
    }

    .field input,
    .field select {
        width: 100%;
        font-size: 0.95rem;
    }

    .check {
        display: flex;
        align-items: center;
        gap: 8px;
        font-size: 0.9rem;
    }

    .checks {
        display: flex;
        flex-wrap: wrap;
        gap: 6px 16px;
    }

    .form .row select {
        min-width: 160px;
    }

    .block {
        margin-bottom: 8px;
    }

    .row.wrap {
        flex-wrap: wrap;
    }

    .row strong {
        font-weight: 600;
    }

    /* Loan cards */
    .loan {
        display: flex;
        flex-direction: column;
        gap: 12px;
    }

    .loan-head {
        display: flex;
        align-items: center;
        gap: 12px;
    }

    .loan-head h3 {
        flex: 1;
        min-width: 0;
        font-size: 1rem;
    }

    .thumb {
        width: 44px;
        height: 44px;
        flex: none;
        object-fit: cover;
        border-radius: var(--radius-sm);
        border: 1px solid var(--line);
    }

    .thumb.sm {
        width: 28px;
        height: 28px;
        border-radius: 6px;
    }

    .rows .row {
        padding: 8px 0;
        font-size: 0.9rem;
    }

    .rows .row > :last-child {
        text-align: right;
        overflow-wrap: anywhere;
    }

    .extend {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 8px;
    }

    .extend input {
        width: 72px;
    }

    /* Printers */
    .printers {
        grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    }

    .printer {
        display: flex;
        flex-direction: column;
        gap: 14px;
    }

    .printer-head {
        display: flex;
        align-items: center;
        gap: 12px;
    }

    .printer-img {
        width: 40px;
        height: 40px;
        object-fit: contain;
    }

    .grow {
        flex: 1;
        min-width: 0;
    }

    .camera {
        display: grid;
        place-items: center;
        aspect-ratio: 16 / 9;
        overflow: hidden;
        border-radius: var(--radius);
        border: 1px solid var(--line);
        background: var(--card-raised);
    }

    .camera img {
        width: 100%;
        height: 100%;
        object-fit: cover;
    }

    .notice {
        display: grid;
        gap: 8px;
        padding: 12px;
        border-radius: var(--radius);
        border: 1px solid color-mix(in srgb, var(--warn) 35%, transparent);
    }

    .notice p {
        margin: 0;
    }

    .notice strong {
        color: var(--warn);
        font-weight: 600;
    }

    .fault {
        display: flex;
        align-items: center;
        gap: 10px;
        color: inherit;
        text-decoration: none;
    }

    .fault:hover .muted {
        color: var(--ctp-text);
    }

    .progress {
        display: flex;
        align-items: center;
        gap: 10px;
    }

    .progress .bar {
        flex: 1;
    }

    .printer p.file {
        margin: 0;
    }

    .ams {
        display: grid;
        gap: 8px;
    }

    .ams-head {
        display: flex;
        align-items: center;
        gap: 10px;
    }

    .slots {
        display: grid;
        grid-template-columns: repeat(4, 1fr);
        gap: 6px;
    }

    .slot {
        display: grid;
        gap: 4px;
        padding: 8px;
        border-radius: var(--radius-sm);
        border: 1px solid var(--line);
        font-size: 0.75rem;
        min-width: 0;
    }

    .slot.empty {
        opacity: 0.5;
    }

    .slot.active {
        border-color: var(--accent);
    }

    .slot-top {
        display: flex;
        align-items: center;
        justify-content: space-between;
    }

    .slot-mat {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .swatch {
        width: 12px;
        height: 12px;
        border-radius: 50%;
        border: 1px solid var(--line-strong);
    }

    .temps {
        display: flex;
        flex-wrap: wrap;
        gap: 14px;
    }

    .panel {
        display: grid;
        gap: 10px;
        padding-top: 14px;
        border-top: 1px solid var(--line);
    }

    .panel p {
        margin: 0;
    }

    .panel .file {
        flex: 1;
        min-width: 0;
    }

    .file-pick {
        position: relative;
    }

    .file-pick input {
        position: absolute;
        width: 1px;
        height: 1px;
        opacity: 0;
    }

    .file-pick:focus-within {
        outline: 2px solid var(--ctp-lavender);
        outline-offset: 2px;
    }

    .swatches {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
    }

    .swatch-btn {
        width: 32px;
        height: 32px;
        border-radius: 50%;
        border: 2px solid var(--line-strong);
        color: var(--ctp-text);
        font-size: 0.75rem;
        cursor: pointer;
    }

    .swatch-btn.picked {
        border-color: var(--accent);
        outline: 2px solid var(--accent-soft);
    }

    code {
        font-family: var(--mono);
        font-size: 0.85em;
    }

    .credits {
        margin-top: 48px;
        text-align: center;
    }

    .credits a {
        color: inherit;
    }

    @media (max-width: 700px) {
        .tabs {
            margin-left: -16px;
            margin-right: -16px;
            padding: 0 8px;
        }

        .slots {
            grid-template-columns: repeat(2, 1fr);
        }
    }
</style>
