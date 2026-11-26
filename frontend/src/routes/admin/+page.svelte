<script>
    import { onMount, onDestroy, tick } from 'svelte';
    import { replaceState } from '$app/navigation';
    import { cameraFeed } from '$lib/camera.js';
    import { api, can, clearSession as clearAuth, getToken, loginUrl, refreshUser } from '$lib/auth.js';
    import BookingPage from '$lib/BookingPage.svelte';
    import { hueFor } from '$lib/palette.js';
    import IconDashboard from '@tabler/icons-svelte-runes/icons/layout-dashboard';
    import IconSearch from '@tabler/icons-svelte-runes/icons/search';
    import IconBuilding from '@tabler/icons-svelte-runes/icons/building-factory-2';
    import IconPrinter from '@tabler/icons-svelte-runes/icons/printer';
    import IconCalendar from '@tabler/icons-svelte-runes/icons/calendar';
    import IconCircleCheck from '@tabler/icons-svelte-runes/icons/circle-check';
    import IconHistory from '@tabler/icons-svelte-runes/icons/history';
    import IconPackage from '@tabler/icons-svelte-runes/icons/package';
    import IconUsers from '@tabler/icons-svelte-runes/icons/users';
    import IconKey from '@tabler/icons-svelte-runes/icons/key';
    import IconLock from '@tabler/icons-svelte-runes/icons/lock';
    import IconLogout from '@tabler/icons-svelte-runes/icons/logout';
    import IconDownload from '@tabler/icons-svelte-runes/icons/download';
    import IconFile from '@tabler/icons-svelte-runes/icons/file';
    import IconFile3d from '@tabler/icons-svelte-runes/icons/file-3d';
    import IconFileCode from '@tabler/icons-svelte-runes/icons/file-code';
    import IconRefresh from '@tabler/icons-svelte-runes/icons/refresh';
    import IconUpload from '@tabler/icons-svelte-runes/icons/upload';
    import IconTrash from '@tabler/icons-svelte-runes/icons/trash';
    import IconPlay from '@tabler/icons-svelte-runes/icons/player-play';
    import IconPause from '@tabler/icons-svelte-runes/icons/player-pause';
    import IconStop from '@tabler/icons-svelte-runes/icons/player-stop';
    import IconBulb from '@tabler/icons-svelte-runes/icons/bulb';
    import IconBulbOff from '@tabler/icons-svelte-runes/icons/bulb-off';
    import IconX from '@tabler/icons-svelte-runes/icons/x';
    import IconExternal from '@tabler/icons-svelte-runes/icons/external-link';
    import IconAlert from '@tabler/icons-svelte-runes/icons/alert-triangle';
    import IconDroplet from '@tabler/icons-svelte-runes/icons/droplet';
    import IconTemperature from '@tabler/icons-svelte-runes/icons/temperature';
    import IconFlame from '@tabler/icons-svelte-runes/icons/flame';
    import IconBed from '@tabler/icons-svelte-runes/icons/bed';
    import IconBox from '@tabler/icons-svelte-runes/icons/box';
    import IconFolder from '@tabler/icons-svelte-runes/icons/folder';
    import IconPencil from '@tabler/icons-svelte-runes/icons/pencil';
    import IconPlus from '@tabler/icons-svelte-runes/icons/plus';
    import IconMerge from '@tabler/icons-svelte-runes/icons/arrow-merge';
    import IconArchive from '@tabler/icons-svelte-runes/icons/archive';
    import IconUser from '@tabler/icons-svelte-runes/icons/user';
    import IconPhoneOff from '@tabler/icons-svelte-runes/icons/phone-off';
    import IconPhoto from '@tabler/icons-svelte-runes/icons/photo';
    import IconShieldLock from '@tabler/icons-svelte-runes/icons/shield-lock';
    import IconBan from '@tabler/icons-svelte-runes/icons/ban';
    import IconCheck from '@tabler/icons-svelte-runes/icons/check';
    import IconRobot from '@tabler/icons-svelte-runes/icons/robot';
    import IconVideo from '@tabler/icons-svelte-runes/icons/video';
    import IconFlask from '@tabler/icons-svelte-runes/icons/flask';
    import IconEraser from '@tabler/icons-svelte-runes/icons/eraser';
    import IconHourglass from '@tabler/icons-svelte-runes/icons/hourglass';

    // Authentication state
    let isLoggedIn = false;
    let adminInfo = null;
    let authToken = '';

    // Current view
    // dashboard, lost-missing, lab-view, printers, bookings, approvals, history,
    // items, users, change-password
    let currentView = 'dashboard';
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

    // The section menu: the side list on wide screens and the drop-down on
    // phones are both drawn from this, in the same order, with the same gates
    $: tabs = !adminInfo
        ? []
        : [
              { key: 'dashboard', label: 'Dashboard', icon: IconDashboard, open: goToDashboard },
              ...(can(adminInfo, 'manage_inventory')
                  ? [
                        { key: 'lost-missing', label: 'Missing items', icon: IconSearch, count: lostMissingItems.length, open: showLostMissingItems },
                        ...labs.map((lab) => ({ key: `lab:${lab}`, label: lab, icon: IconBuilding, open: () => showLabView(lab) }))
                    ]
                  : []),
              ...(can(adminInfo, 'manage_printers') ? [{ key: 'printers', label: 'Printers', icon: IconPrinter, open: showPrinters }] : []),
              ...(can(adminInfo, 'manage_bookings') ? [{ key: 'bookings', label: 'Bookings', icon: IconCalendar, open: showBookings }] : []),
              ...(isApprover ? [{ key: 'approvals', label: 'Approvals', icon: IconCircleCheck, count: approvalCount, open: showApprovals }] : []),
              ...(can(adminInfo, 'manage_inventory')
                  ? [
                        { key: 'history', label: 'Item history', icon: IconHistory, open: showItemHistory },
                        { key: 'items', label: 'Items', icon: IconPackage, open: showItems }
                    ]
                  : []),
              ...(can(adminInfo, 'manage_users') ? [{ key: 'users', label: 'Users & groups', icon: IconUsers, count: pendingUsers.length, open: showUsers }] : []),
              { key: 'change-password', label: 'Change password', icon: IconKey, open: showChangePasswordView }
          ];
    $: currentTabKey = currentView === 'lab-view' ? `lab:${selectedLab}` : currentView;

    function selectTab(key) {
        tabs.find((t) => t.key === key)?.open();
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

    // The play button next to a file: open the start form with that file chosen
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

    // Tabler badge colour for a printer state and a print log result
    const STATE_BADGE = { running: 'blue', failed: 'red', paused: 'yellow', finished: 'green', idle: 'secondary', offline: 'secondary' };
    const RESULT_BADGE = { finished: 'green', failed: 'red', stopped: 'orange', running: 'blue' };

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
    <div class="toast-msg {messageType}" role="status">{message}</div>
{/if}

{#snippet acctBadge(loan)}
    <span
        class="badge {loan.user_id ? 'bg-green-lt' : 'bg-secondary-lt'} ms-1"
        title={loan.user_id ? 'Borrowed while signed in' : 'Borrowed without an account: reach them on this phone number'}
    >
        {#if loan.user_id}<IconUser size={14} class="icon me-1" />account{:else}<IconPhoneOff size={14} class="icon me-1" />no account{/if}
    </span>
{/snippet}

{#snippet phone(number)}
    {#if number}
        <button type="button" class="btn btn-link p-0 text-reset text-decoration-underline tabular" on:click={() => copyToClipboard(number)} title="Copy phone number">{number}</button>
    {:else}
        <span class="text-secondary">-</span>
    {/if}
{/snippet}

{#snippet thumb(filename, name)}
    {#if filename}
        <img class="thumb" src="/api/photos/{filename}" alt={name} on:error={(e) => (e.target.style.display = 'none')} />
    {:else}
        <span class="avatar avatar-sm bg-secondary-lt" aria-hidden="true"><IconPhoto size={18} /></span>
    {/if}
{/snippet}

{#snippet when(b)}
    <div class="text-nowrap">{new Date(b.start_time).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}</div>
    <div class="small text-secondary text-nowrap tabular">
        {new Date(b.start_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
        -
        {new Date(b.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
    </div>
{/snippet}

{#snippet bookingWhat(b)}
    <div class="d-flex align-items-center gap-2">
        <span class="res-dot" style="background: {hueFor(b.resource_id)}" aria-hidden="true"></span>
        <span class="fw-medium">{resourceName[b.resource_id] || 'Unknown resource'}</span>
    </div>
    {#if b.thread_name}
        <div class="small text-secondary"><IconFlask size={14} class="icon" /> {b.thread_name}</div>
    {/if}
{/snippet}

{#if !isLoggedIn}
    <div class="empty">
        <p class="empty-title">Checking your account...</p>
    </div>
{:else if !hasAdminArea}
    <!-- Signed in, but nothing here is theirs to manage -->
    <div class="page-header d-print-none mb-3">
        <h2 class="page-title">Admin</h2>
    </div>
    <div class="card">
        <div class="empty">
            <div class="empty-icon"><IconLock size={48} class="icon" /></div>
            <p class="empty-title">No admin access</p>
            <p class="empty-subtitle text-secondary">
                Hi {adminInfo.name}. Your account{adminInfo.group ? ` (${adminInfo.group})` : ''} doesn't have any admin
                permissions, so there is nothing to manage here. If you think you should have access, ask a server admin to
                change your group.
            </p>
            <div class="empty-action btn-list justify-content-center">
                <a class="btn btn-primary" href="/dashboard">Go to your dashboard</a>
                <a class="btn btn-outline-secondary" href="/account">Your account</a>
            </div>
        </div>
    </div>
{:else}
    <div class="page-header d-print-none mb-3">
        <div class="row g-2 align-items-center">
            <div class="col">
                <div class="page-pretitle">
                    Signed in as {adminInfo.name}
                    {#if adminInfo.group}<span class="badge bg-primary-lt ms-1">{adminInfo.group}</span>{/if}
                </div>
                <h2 class="page-title">Admin</h2>
            </div>
            <div class="col-auto">
                <button type="button" class="btn btn-outline-secondary" on:click={logout}>
                    <IconLogout size={18} class="icon" />
                    Log out
                </button>
            </div>
        </div>
    </div>

    <div class="row g-3">
        <!-- Section menu: a drop-down on phones, a side list on wide screens -->
        <div class="col-12 col-lg-3">
            <div class="d-lg-none">
                <label class="form-label" for="admin-section">Section</label>
                <select id="admin-section" class="form-select" value={currentTabKey} on:change={(e) => selectTab(e.target.value)}>
                    {#each tabs as tab (tab.key)}
                        <option value={tab.key}>{tab.label}{tab.count ? ` (${tab.count})` : ''}</option>
                    {/each}
                </select>
            </div>
            <nav class="card d-none d-lg-block admin-nav" aria-label="Admin sections">
                <div class="list-group list-group-flush">
                    {#each tabs as tab (tab.key)}
                        <button
                            type="button"
                            class="list-group-item list-group-item-action d-flex align-items-center gap-2"
                            class:active={currentTabKey === tab.key}
                            aria-current={currentTabKey === tab.key ? 'page' : undefined}
                            on:click={tab.open}
                        >
                            <svelte:component this={tab.icon} size={18} class="icon text-secondary" />
                            <span class="flex-fill text-start">{tab.label}</span>
                            {#if tab.count}<span class="badge bg-red-lt">{tab.count}</span>{/if}
                        </button>
                    {/each}
                </div>
            </nav>
        </div>

        <div class="col-12 col-lg-9">
            <!-- Dashboard -->
            {#if currentView === 'dashboard'}
                <div class="row row-cards g-3">
                    {#if can(adminInfo, 'manage_users')}
                        <div class="col-12 col-sm-6 col-xl-4">
                            <div class="card card-sm h-100">
                                {#if pendingUsers.length > 0}<div class="card-status-top bg-warning"></div>{/if}
                                <div class="card-body d-flex flex-column">
                                    <div class="subheader">Pending sign-ups</div>
                                    <div class="h1 my-2 tabular">{pendingUsers.length}</div>
                                    <div class="mt-auto"><button type="button" class="btn btn-outline-primary btn-sm" on:click={showUsers}>Review</button></div>
                                </div>
                            </div>
                        </div>
                    {/if}
                    {#if isApprover}
                        <div class="col-12 col-sm-6 col-xl-4">
                            <div class="card card-sm h-100">
                                {#if approvalCount > 0}<div class="card-status-top bg-warning"></div>{/if}
                                <div class="card-body d-flex flex-column">
                                    <div class="subheader">Approvals</div>
                                    <div class="h1 my-2 tabular">{approvalCount}</div>
                                    <div class="mt-auto"><button type="button" class="btn btn-outline-primary btn-sm" on:click={showApprovals}>Review</button></div>
                                </div>
                            </div>
                        </div>
                    {/if}
                    {#if can(adminInfo, 'manage_inventory')}
                        <div class="col-12 col-sm-6 col-xl-4">
                            <div class="card card-sm h-100">
                                <div class="card-body d-flex flex-column">
                                    <div class="subheader">Missing items</div>
                                    <div class="h1 my-2 tabular">{lostMissingItems.length}</div>
                                    <div class="mt-auto"><button type="button" class="btn btn-outline-primary btn-sm" on:click={showLostMissingItems}>Review</button></div>
                                </div>
                            </div>
                        </div>
                        <div class="col-12 col-sm-6 col-xl-4">
                            <div class="card card-sm h-100">
                                <div class="card-body d-flex flex-column">
                                    <div class="subheader">Item catalogue</div>
                                    <div class="h1 my-2 tabular">{catalogue.length}</div>
                                    <div class="mt-auto"><button type="button" class="btn btn-outline-primary btn-sm" on:click={showItems}>Manage</button></div>
                                </div>
                            </div>
                        </div>
                    {/if}
                    {#if can(adminInfo, 'manage_printers')}
                        <div class="col-12 col-sm-6 col-xl-4">
                            <div class="card card-sm h-100">
                                <div class="card-body d-flex flex-column">
                                    <div class="d-flex align-items-center gap-2 mb-1">
                                        <span class="avatar avatar-sm bg-blue-lt"><IconPrinter size={18} /></span>
                                        <h3 class="card-title mb-0">3D printers</h3>
                                    </div>
                                    <p class="text-secondary small mb-3">Live status, files on each printer, start and stop prints</p>
                                    <div class="mt-auto"><button type="button" class="btn btn-outline-primary btn-sm" on:click={showPrinters}>Manage</button></div>
                                </div>
                            </div>
                        </div>
                    {/if}
                    {#if can(adminInfo, 'manage_bookings')}
                        <div class="col-12 col-sm-6 col-xl-4">
                            <div class="card card-sm h-100">
                                <div class="card-body d-flex flex-column">
                                    <div class="d-flex align-items-center gap-2 mb-1">
                                        <span class="avatar avatar-sm bg-purple-lt"><IconCalendar size={18} /></span>
                                        <h3 class="card-title mb-0">Bookings</h3>
                                    </div>
                                    <p class="text-secondary small mb-3">Mocap Lab and Mech Lab robots</p>
                                    <div class="mt-auto"><button type="button" class="btn btn-outline-primary btn-sm" on:click={showBookings}>Manage</button></div>
                                </div>
                            </div>
                        </div>
                    {/if}
                    {#if can(adminInfo, 'manage_inventory')}
                        {#each labs as lab}
                            <div class="col-12 col-sm-6 col-xl-4">
                                <div class="card card-sm h-100">
                                    <div class="card-body d-flex flex-column">
                                        <div class="d-flex align-items-center gap-2 mb-1">
                                            <span class="avatar avatar-sm bg-azure-lt"><IconBuilding size={18} /></span>
                                            <h3 class="card-title mb-0">{lab}</h3>
                                        </div>
                                        <p class="text-secondary small mb-3">Manage borrowed items</p>
                                        <div class="mt-auto"><button type="button" class="btn btn-outline-primary btn-sm" on:click={() => showLabView(lab)}>View</button></div>
                                    </div>
                                </div>
                            </div>
                        {/each}
                    {/if}

                    {#if can(adminInfo, 'manage_inventory') || can(adminInfo, 'manage_bookings') || can(adminInfo, 'manage_printers')}
                        <div class="col-12">
                            <div class="card">
                                <div class="card-header">
                                    <h3 class="card-title">Exports</h3>
                                </div>
                                <div class="card-body">
                                    <div class="btn-list">
                                        {#if can(adminInfo, 'manage_inventory')}
                                            <button type="button" class="btn btn-outline-secondary" on:click={() => exportCSV()}>
                                                <IconDownload size={18} class="icon" />Loans (CSV)
                                            </button>
                                        {/if}
                                        {#if can(adminInfo, 'manage_bookings')}
                                            <button type="button" class="btn btn-outline-secondary" on:click={() => exportCSV('/api/admin/export-bookings-csv', 'motion_capture_lab_bookings.csv')}>
                                                <IconDownload size={18} class="icon" />Bookings (CSV)
                                            </button>
                                        {/if}
                                        {#if can(adminInfo, 'manage_printers')}
                                            <button type="button" class="btn btn-outline-secondary" on:click={() => exportCSV('/api/admin/export-print-jobs-csv', 'print_jobs.csv')}>
                                                <IconDownload size={18} class="icon" />Print log (CSV)
                                            </button>
                                        {/if}
                                    </div>
                                </div>
                            </div>
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Missing items -->
            {#if currentView === 'lost-missing'}
                <div class="card">
                    <div class="card-header">
                        <div>
                            <h3 class="card-title">Missing items</h3>
                            <p class="card-subtitle">Items an admin could not find in the lab</p>
                        </div>
                    </div>
                    {#if loading}
                        <div class="card-body text-secondary">Loading missing items...</div>
                    {:else if lostMissingItems.length === 0}
                        <div class="empty">
                            <div class="empty-icon"><IconCircleCheck size={40} class="icon" /></div>
                            <p class="empty-title">No missing items</p>
                        </div>
                    {:else}
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr>
                                        <th>Item</th>
                                        <th>Borrower</th>
                                        <th>Phone</th>
                                        <th>Lab</th>
                                        <th>Qty</th>
                                        <th>Expected return</th>
                                        <th>Borrowed</th>
                                        <th class="w-1"></th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {#each lostMissingItems as loan}
                                        <tr>
                                            <td>
                                                <div class="d-flex align-items-center gap-2">
                                                    {@render thumb(loan.photo_filename, loan.item_name)}
                                                    <div>
                                                        <div class="fw-medium">{loan.item_name}</div>
                                                        <div class="small text-secondary">
                                                            <span class="badge bg-red-lt">Missing</span>
                                                            {#if loan.approved_by}marked by {loan.approved_by}{/if}
                                                        </div>
                                                    </div>
                                                </div>
                                            </td>
                                            <td class="text-nowrap">{loan.borrower_name}{@render acctBadge(loan)}</td>
                                            <td>{@render phone(loan.borrower_phone)}</td>
                                            <td class="text-nowrap">{loan.lab_location}</td>
                                            <td class="tabular">{loan.quantity_borrowed}</td>
                                            <td class="text-nowrap">{formatExpectedReturn(loan.expected_return_date)}</td>
                                            <td class="text-nowrap">{formatDate(loan.CreatedAt)}</td>
                                            <td>
                                                {#if loan.status === 'not_found'}
                                                    <button type="button" class="btn btn-sm btn-outline-success text-nowrap" on:click={() => markAsFound(loan.ID)} disabled={loading}>
                                                        <IconCheck size={16} class="icon" />Mark as found
                                                    </button>
                                                {/if}
                                            </td>
                                        </tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- 3D printers -->
            {#if currentView === 'printers'}
                <div class="d-flex flex-wrap align-items-end justify-content-between gap-2 mb-3">
                    <div>
                        <h3 class="mb-1">3D printers</h3>
                        <div class="text-secondary small">Live status, refreshing every few seconds. Each printer keeps its own files.</div>
                    </div>
                    <a class="btn btn-outline-secondary btn-sm" href="/printers" target="_blank" rel="noopener">
                        <IconExternal size={16} class="icon" />Open the full page
                    </a>
                </div>

                {#if printers.length === 0}
                    <div class="card">
                        <div class="empty">
                            <div class="empty-icon"><IconPrinter size={40} class="icon" /></div>
                            <p class="empty-title">{printersLoaded ? 'No printers are configured.' : 'Loading printers...'}</p>
                        </div>
                    </div>
                {:else}
                    <div class="row row-cards g-3">
                        {#each printers as printer (printer.id)}
                            {@const files = uploadFiles[printer.id]}
                            {@const stateKey = printerStateClass(printer)}
                            <div class="col-12 col-xl-6">
                                <div class="card h-100">
                                    <div class="card-status-top bg-{STATE_BADGE[stateKey]}"></div>
                                    <div class="card-header">
                                        <div class="d-flex align-items-center gap-2 min-w-0">
                                            <img src="/P1S.png" alt="" class="printer-img" />
                                            <div class="min-w-0">
                                                <h3 class="card-title text-truncate">{printer.name}</h3>
                                                <span class="badge {printer.online ? (printerIsPrinting(printer) ? 'bg-orange-lt' : 'bg-green-lt') : 'bg-secondary-lt'}">
                                                    {printer.online ? (printerIsPrinting(printer) ? 'In use' : 'Free') : 'Unknown'}
                                                </span>
                                            </div>
                                        </div>
                                        <div class="card-actions">
                                            <span class="badge bg-{STATE_BADGE[stateKey]}-lt">{printerStateLabel(printer)}</span>
                                        </div>
                                    </div>

                                    <div class="card-body">
                                        <div class="camera mb-3">
                                            {#if printer.camera_online && $camera[printer.id]}
                                                <img src={$camera[printer.id]} alt="Camera view of {printer.name}" />
                                            {:else}
                                                <div class="camera-empty text-secondary">
                                                    <img src="/P1S.png" alt="" />
                                                    <span class="small">{printer.camera_online ? 'Loading camera...' : 'No camera image'}</span>
                                                </div>
                                            {/if}
                                        </div>

                                        {#if printer.faults && printer.faults.length > 0}
                                            <div class="alert alert-danger" role="alert">
                                                <div class="d-flex gap-2">
                                                    <IconAlert size={20} class="icon alert-icon" />
                                                    <div class="flex-fill">
                                                        <h4 class="alert-title">Printer reported {printer.faults.length} fault{printer.faults.length > 1 ? 's' : ''}</h4>
                                                        <div class="list-group list-group-flush">
                                                            {#each printer.faults as fault}
                                                                <a class="list-group-item list-group-item-action d-flex align-items-center gap-2 px-0 bg-transparent" href={fault.url} target="_blank" rel="noopener">
                                                                    <span class="badge {fault.severity === 'fatal' || fault.severity === 'serious' ? 'bg-red-lt' : 'bg-yellow-lt'}">{fault.severity}</span>
                                                                    <code>{fault.code}</code>
                                                                    <span class="ms-auto small text-nowrap">Look up <IconExternal size={14} class="icon" /></span>
                                                                </a>
                                                            {/each}
                                                        </div>
                                                    </div>
                                                </div>
                                            </div>
                                        {/if}

                                        {#if printer.access_code_problem}
                                            <div class="alert alert-warning" role="alert">
                                                <div class="d-flex gap-2">
                                                    <IconAlert size={20} class="icon alert-icon" />
                                                    <div class="flex-fill min-w-0">
                                                        <h4 class="alert-title">Access code changed</h4>
                                                        <div class="text-secondary mb-2">{printer.name} is refusing our access code. LAN mode was probably toggled.</div>
                                                        {#if editingCode === printer.id}
                                                            <div class="input-group">
                                                                <input class="form-control" type="text" bind:value={newCode} placeholder="New access code" autocomplete="off" aria-label="New access code for {printer.name}" />
                                                                <button type="button" class="btn btn-primary" on:click={() => savePrinterCode(printer)} disabled={savingCode}>{savingCode ? 'Saving...' : 'Save'}</button>
                                                                <button type="button" class="btn btn-outline-secondary" on:click={() => (editingCode = '')}>Cancel</button>
                                                            </div>
                                                        {:else}
                                                            <button type="button" class="btn btn-warning btn-sm" on:click={() => { editingCode = printer.id; newCode = ''; }}>Update access code</button>
                                                        {/if}
                                                    </div>
                                                </div>
                                            </div>
                                        {/if}

                                        {#if printer.online}
                                            {#if printerIsPrinting(printer)}
                                                <div class="d-flex align-items-center gap-2 mb-1">
                                                    <div class="progress progress-sm flex-fill">
                                                        <div class="progress-bar" style="width: {printer.progress}%" role="progressbar" aria-valuenow={printer.progress} aria-valuemin="0" aria-valuemax="100" aria-label="Print progress"></div>
                                                    </div>
                                                    <span class="fw-medium tabular">{printer.progress}%</span>
                                                </div>
                                                {#if printer.remaining_minutes > 0}
                                                    <div class="small text-secondary mb-2">{formatRemaining(printer.remaining_minutes)}</div>
                                                {/if}
                                            {/if}

                                            {#if printer.file_name}
                                                <div class="d-flex align-items-center gap-1 mb-2 min-w-0" title={printer.file_name}>
                                                    <IconFile size={16} class="icon text-secondary flex-shrink-0" />
                                                    <span class="text-truncate">{printer.file_name}</span>
                                                </div>
                                            {/if}

                                            {#if (printer.ams && printer.ams.length > 0) || printer.external_spool}
                                                <div class="mb-3">
                                                    {#each printer.ams || [] as unit}
                                                        <div class="d-flex align-items-center gap-3 small mb-1">
                                                            <span class="fw-medium">AMS {unit.id + 1}</span>
                                                            {#if unit.humidity}
                                                                <span class="text-secondary" title="The AMS reports humidity on its own 1-5 scale"><IconDroplet size={14} class="icon" /> {unit.humidity}/5</span>
                                                            {/if}
                                                            {#if unit.temp}
                                                                <span class="text-secondary"><IconTemperature size={14} class="icon" /> {unit.temp}°C</span>
                                                            {/if}
                                                        </div>
                                                        <div class="ams-slots mb-2">
                                                            {#each unit.slots || [] as slot}
                                                                <div
                                                                    class="ams-slot"
                                                                    class:empty={slot.empty}
                                                                    class:active={slot.active}
                                                                    title={slot.empty
                                                                        ? `Slot ${slot.slot}: empty`
                                                                        : `Slot ${slot.slot}: ${slot.material}${slot.remain >= 0 ? ', ' + slot.remain + '% left' : ', amount unknown'}${slot.active ? ' (in use)' : ''}`}
                                                                >
                                                                    <span class="d-flex align-items-center gap-1">
                                                                        <span class="swatch" style="background: {slot.color || 'transparent'}"></span>
                                                                        <span class="small text-secondary">{slot.slot}</span>
                                                                        {#if slot.active}<span class="status-dot status-dot-animated status-green ms-auto" title="Currently feeding"></span>{/if}
                                                                    </span>
                                                                    <span class="small fw-medium text-truncate">{slot.empty ? 'Empty' : slot.material}</span>
                                                                    {#if slot.remain >= 0}
                                                                        <div class="progress progress-sm"><div class="progress-bar" style="width: {slot.remain}%"></div></div>
                                                                        <span class="small text-secondary tabular">{slot.remain}%</span>
                                                                    {:else if !slot.empty}
                                                                        <span class="small text-secondary" title="No RFID tag, so the printer cannot tell">? left</span>
                                                                    {/if}
                                                                </div>
                                                            {/each}
                                                        </div>
                                                    {/each}

                                                    {#if printer.external_spool && !printer.external_spool.empty}
                                                        <div class="d-flex align-items-center gap-2 small">
                                                            <span class="swatch" style="background: {printer.external_spool.color || 'transparent'}"></span>
                                                            <span class="text-secondary">External spool</span>
                                                            <span class="fw-medium">{printer.external_spool.material}</span>
                                                            {#if printer.external_spool.remain >= 0}
                                                                <span class="text-secondary tabular">{printer.external_spool.remain}%</span>
                                                            {/if}
                                                        </div>
                                                    {/if}
                                                </div>
                                            {/if}

                                            <div class="d-flex flex-wrap gap-3 mb-3 tabular">
                                                <span title="Nozzle"><IconFlame size={16} class="icon text-secondary" /> {printer.nozzle_temp.toFixed(0)}°C</span>
                                                <span title="Bed"><IconBed size={16} class="icon text-secondary" /> {printer.bed_temp.toFixed(0)}°C</span>
                                                {#if printer.chamber_temp > 0}
                                                    <span title="Chamber"><IconBox size={16} class="icon text-secondary" /> {printer.chamber_temp.toFixed(0)}°C</span>
                                                {/if}
                                            </div>

                                            <div class="btn-list mb-3">
                                                <button
                                                    type="button"
                                                    class="btn {printer.light_on ? 'btn-warning' : 'btn-outline-secondary'}"
                                                    aria-pressed={printer.light_on}
                                                    on:click={() => toggleLight(printer)}
                                                    disabled={busyPrinter === printer.id + 'light'}
                                                    title="Chamber light: the camera is useless in the dark"
                                                >
                                                    {#if printer.light_on}<IconBulb size={18} class="icon" />Light on{:else}<IconBulbOff size={18} class="icon" />Light off{/if}
                                                </button>

                                                {#if printerIsPrinting(printer)}
                                                    <button type="button" class="btn btn-outline-secondary" on:click={() => pausePrint(printer)} disabled={busyPrinter === printer.id + 'pause'}>
                                                        <IconPause size={18} class="icon" />Pause
                                                    </button>
                                                {:else if printer.state === 'PAUSE'}
                                                    <button type="button" class="btn btn-outline-primary" on:click={() => resumePrint(printer)} disabled={busyPrinter === printer.id + 'resume'}>
                                                        <IconPlay size={18} class="icon" />Resume
                                                    </button>
                                                {/if}

                                                {#if printerIsPrinting(printer) || printer.state === 'PAUSE'}
                                                    <button type="button" class="btn btn-danger" on:click={() => stopPrint(printer)} disabled={stoppingPrinter === printer.id}>
                                                        <IconStop size={18} class="icon" />{stoppingPrinter === printer.id ? 'Stopping...' : 'Stop print'}
                                                    </button>
                                                {/if}
                                            </div>
                                        {:else}
                                            <p class="text-secondary">Not responding: switched off or off the network. Files can only be listed while it is on.</p>
                                        {/if}

                                        <!-- This printer's own files -->
                                        <div class="btn-list">
                                            <button type="button" class="btn btn-outline-secondary" aria-expanded={!!filesOpen[printer.id]} on:click={() => toggleUpload(printer)}>
                                                {#if filesOpen[printer.id]}
                                                    <IconX size={18} class="icon" />Close files
                                                {:else}
                                                    <IconFolder size={18} class="icon" />Files{files ? ` (${files.length})` : ''} and upload
                                                {/if}
                                            </button>
                                            {#if printer.online && canStart(printer)}
                                                <button type="button" class="btn {startFor === printer.id ? 'btn-outline-secondary' : 'btn-primary'}" aria-expanded={startFor === printer.id} on:click={() => toggleStart(printer)}>
                                                    {#if startFor === printer.id}<IconX size={18} class="icon" />Close{:else}<IconPlay size={18} class="icon" />Start a print{/if}
                                                </button>
                                            {/if}
                                        </div>

                                        {#if filesOpen[printer.id]}
                                            <div class="border rounded p-3 mt-3">
                                                <div class="d-flex align-items-center justify-content-between gap-2 mb-2">
                                                    <h4 class="mb-0">{printer.name}: files</h4>
                                                    <button type="button" class="btn btn-ghost-secondary btn-sm" on:click={() => loadPrinterFiles(printer, true)} disabled={listingFiles === printer.id} title="Reload the list from the printer">
                                                        <IconRefresh size={16} class="icon" />{listingFiles === printer.id ? 'Loading...' : 'Refresh'}
                                                    </button>
                                                </div>
                                                <p class="form-hint mb-2">
                                                    File names should start with the owner's name, e.g. <code>srinath_bracket.3mf</code>. The file lands on
                                                    <strong>{printer.name}</strong> only. Start it below or from the printer's screen once the plate is clear.
                                                </p>

                                                <label class="form-label" for="upload-{printer.id}">
                                                    <IconUpload size={16} class="icon" />
                                                    {uploading === printer.id ? `Sending... ${uploadProgress}%` : 'Send a .3mf or .gcode file'}
                                                </label>
                                                <input id="upload-{printer.id}" class="form-control mb-2" type="file" accept=".3mf,.gcode" on:change={(e) => sendFile(printer, e)} disabled={uploading === printer.id} />

                                                {#if uploading === printer.id}
                                                    <div class="progress progress-sm mb-2">
                                                        <div class="progress-bar" style="width: {uploadProgress}%" role="progressbar" aria-valuenow={uploadProgress} aria-valuemin="0" aria-valuemax="100" aria-label="Upload progress"></div>
                                                    </div>
                                                {/if}

                                                {#if fileErrors[printer.id]}
                                                    <div class="alert alert-danger py-2 mb-2" role="alert">{fileErrors[printer.id]}</div>
                                                {/if}

                                                {#if !files}
                                                    {#if !fileErrors[printer.id]}<p class="text-secondary mb-0">Loading the file list...</p>{/if}
                                                {:else if files.length === 0}
                                                    <p class="text-secondary mb-0">No printable files on this printer.</p>
                                                {:else}
                                                    <div class="small text-secondary mb-1">{files.length} file{files.length === 1 ? '' : 's'}, {fileSize(files.reduce((n, f) => n + (f.size || 0), 0))}</div>
                                                    <div class="list-group list-group-flush border-top border-bottom mb-2">
                                                        {#each files as file (file.name)}
                                                            <div class="list-group-item d-flex align-items-center gap-2 px-0">
                                                                {#if file.name.toLowerCase().endsWith('.gcode')}
                                                                    <IconFileCode size={20} class="icon text-secondary flex-shrink-0" />
                                                                {:else}
                                                                    <IconFile3d size={20} class="icon text-secondary flex-shrink-0" />
                                                                {/if}
                                                                <div class="flex-fill min-w-0">
                                                                    <div class="text-truncate" title={file.name}>{file.name}</div>
                                                                    <div class="small text-secondary">{fileSize(file.size)}{#if file.time}, {file.time}{/if}</div>
                                                                </div>
                                                                {#if canStart(printer)}
                                                                    <button type="button" class="btn btn-ghost-primary btn-icon" on:click={() => printThisFile(printer, file.name)} title="Start {file.name} on {printer.name}" aria-label="Print {file.name}">
                                                                        <IconPlay size={18} />
                                                                    </button>
                                                                {/if}
                                                                <button type="button" class="btn btn-ghost-danger btn-icon" on:click={() => deletePrinterFile(printer, file.name)} title="Delete from {printer.name}" aria-label="Delete {file.name}">
                                                                    <IconTrash size={18} />
                                                                </button>
                                                            </div>
                                                        {/each}
                                                    </div>

                                                    <button type="button" class="btn btn-outline-danger btn-sm" on:click={() => clearPrinterFiles(printer)} disabled={clearing === printer.id}>
                                                        <IconEraser size={16} class="icon" />{clearing === printer.id ? 'Clearing...' : `Clear all ${files.length} file(s)`}
                                                    </button>
                                                {/if}
                                            </div>
                                        {/if}

                                        {#if printer.online}
                                            {#if startFor === printer.id}
                                                <div class="border rounded p-3 mt-3" id="start-{printer.id}">
                                                    <h4 class="mb-3">Start a print</h4>
                                                    <div class="row g-2">
                                                        <div class="col-12">
                                                            <label class="form-label" for="start-file-{printer.id}">File</label>
                                                            <select id="start-file-{printer.id}" class="form-select" bind:value={startFile}>
                                                                <option value="">{files ? (files.length ? 'Choose a file...' : 'No files on this printer') : 'Loading files...'}</option>
                                                                {#each files || [] as file}
                                                                    <option value={file.name}>{file.name}</option>
                                                                {/each}
                                                            </select>
                                                        </div>
                                                        <div class="col-12 col-sm-6">
                                                            <label class="form-label" for="start-bed-{printer.id}">Plate fitted</label>
                                                            <select id="start-bed-{printer.id}" class="form-select" bind:value={startBed}>
                                                                {#each BED_TYPES as bed}<option value={bed.value}>{bed.label}</option>{/each}
                                                            </select>
                                                        </div>
                                                        <div class="col-12 col-sm-6">
                                                            <label class="form-label" for="start-plate-{printer.id}">Plate number</label>
                                                            <input id="start-plate-{printer.id}" class="form-control" type="number" min="1" max="64" bind:value={startPlate} />
                                                        </div>
                                                        <div class="col-12">
                                                            <label class="form-label" for="start-spool-{printer.id}">Filament</label>
                                                            <select id="start-spool-{printer.id}" class="form-select" bind:value={startSpool}>
                                                                {#each amsChoices(printer) as choice}
                                                                    <option value={String(choice.index)}>
                                                                        AMS {choice.unit} slot {choice.slot}: {choice.label}{choice.remain >= 0 ? ` (${choice.remain}%)` : ''}
                                                                    </option>
                                                                {/each}
                                                                <option value="external">External spool</option>
                                                            </select>
                                                        </div>
                                                    </div>

                                                    {#if startSpool !== 'external'}
                                                        <div class="d-flex flex-wrap gap-2 mt-2">
                                                            {#each amsChoices(printer) as choice}
                                                                <button
                                                                    type="button"
                                                                    class="pick-swatch"
                                                                    class:picked={startSpool === String(choice.index)}
                                                                    style="background: {choice.color || 'var(--tblr-secondary)'}"
                                                                    title="AMS {choice.unit} slot {choice.slot}: {choice.label}"
                                                                    aria-label="AMS {choice.unit} slot {choice.slot}: {choice.label}"
                                                                    on:click={() => (startSpool = String(choice.index))}
                                                                >{choice.slot}</button>
                                                            {/each}
                                                        </div>
                                                    {/if}

                                                    <div class="mt-3">
                                                        <label class="form-check form-check-inline">
                                                            <input class="form-check-input" type="checkbox" bind:checked={startLevel} />
                                                            <span class="form-check-label">Bed levelling</span>
                                                        </label>
                                                        <label class="form-check form-check-inline">
                                                            <input class="form-check-input" type="checkbox" bind:checked={startFlow} />
                                                            <span class="form-check-label">Flow calibration</span>
                                                        </label>
                                                        <label class="form-check form-check-inline">
                                                            <input class="form-check-input" type="checkbox" bind:checked={startTimelapse} />
                                                            <span class="form-check-label">Timelapse</span>
                                                        </label>
                                                    </div>

                                                    <div class="alert alert-warning py-2 my-3" role="alert">
                                                        Check the camera above: the plate must be clear and the plate type must match what you chose.
                                                    </div>

                                                    <button type="button" class="btn btn-primary w-100" on:click={() => startPrint(printer)} disabled={starting === printer.id || !startFile}>
                                                        <IconPlay size={18} class="icon" />{starting === printer.id ? 'Starting...' : 'Start print'}
                                                    </button>
                                                </div>
                                            {/if}

                                            {#if printer.last_action_by}
                                                <p class="small text-secondary mt-3 mb-0">Last stopped by {printer.last_action_by}</p>
                                            {/if}
                                        {/if}
                                    </div>
                                </div>
                            </div>
                        {/each}
                    </div>
                {/if}

                <div class="card mt-3">
                    <div class="card-header flex-wrap gap-2">
                        <div class="me-auto">
                            <h3 class="card-title">Print log</h3>
                            <p class="card-subtitle">Recorded automatically from the printers; nobody fills in a form. Whose print it is comes from the file name.</p>
                        </div>
                        <div class="d-flex gap-2">
                            <select class="form-select form-select-sm w-auto" bind:value={jobPrinter} aria-label="Show prints from">
                                <option value="">All printers</option>
                                {#each jobPrinters as name}<option value={name}>{name}</option>{/each}
                            </select>
                            <!-- A plain link would go without the session token and get a 401 -->
                            <button type="button" class="btn btn-outline-secondary btn-sm text-nowrap" on:click={() => exportCSV('/api/admin/export-print-jobs-csv', 'print_jobs.csv')}>
                                <IconDownload size={16} class="icon" />Export CSV
                            </button>
                        </div>
                    </div>
                    {#if shownJobs.length === 0}
                        <div class="empty">
                            <p class="empty-title">No prints recorded yet.</p>
                        </div>
                    {:else}
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr>
                                        <th>When</th>
                                        <th>Printer</th>
                                        <th>File</th>
                                        <th>Duration</th>
                                        <th>Result</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {#each shownJobs.slice(0, showAllJobs ? shownJobs.length : 40) as job}
                                        <tr>
                                            <td class="text-nowrap tabular">
                                                {new Date(job.started_at).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}
                                                {new Date(job.started_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                            </td>
                                            <td class="text-nowrap">{job.printer_name}</td>
                                            <td class="file-cell"><div class="text-truncate" title={job.file_name}>{job.file_name || '(unnamed)'}</div></td>
                                            <td class="text-nowrap tabular">{jobDuration(job)}</td>
                                            <td><span class="badge bg-{RESULT_BADGE[job.result] || 'secondary'}-lt">{jobResult(job)}</span></td>
                                        </tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                        {#if shownJobs.length > 40}
                            <div class="card-footer text-center">
                                <button type="button" class="btn btn-ghost-primary btn-sm" on:click={() => (showAllJobs = !showAllJobs)}>
                                    {showAllJobs ? 'Show the latest 40' : `Show all ${shownJobs.length} prints`}
                                </button>
                            </div>
                        {/if}
                    {/if}
                </div>
            {/if}

            <!-- Bookings -->
            {#if currentView === 'bookings'}
                <div class="card mb-3">
                    <div class="card-header flex-wrap gap-2">
                        <div class="me-auto">
                            <h3 class="card-title">Lab bookings</h3>
                            <p class="card-subtitle">The Motion Capture Lab and the Mech Lab robots.</p>
                        </div>
                        <a class="btn btn-outline-secondary btn-sm" href={bookingLab === 'mocap' ? '/mocap' : '/mechlab'} target="_blank" rel="noopener">
                            <IconExternal size={16} class="icon" />Open calendar view
                        </a>
                    </div>
                    <div class="card-body">
                        <div class="scroll-x mb-3">
                            <div class="nav nav-segmented" role="tablist">
                                <button type="button" class="nav-link" role="tab" aria-selected={bookingLab === 'mocap'} class:active={bookingLab === 'mocap'} on:click={() => (bookingLab = 'mocap')}>
                                    <IconVideo size={18} class="icon me-1" />Mocap Lab
                                </button>
                                <button type="button" class="nav-link" role="tab" aria-selected={bookingLab === 'mech'} class:active={bookingLab === 'mech'} on:click={() => (bookingLab = 'mech')}>
                                    <IconRobot size={18} class="icon me-1" />Mech Lab robots
                                </button>
                            </div>
                        </div>
                        {#key `${bookingLab}-${calendarVersion}`}
                            <BookingPage lab={bookingLab} title={bookingLab === 'mocap' ? 'Motion Capture Lab' : 'Mech Lab robots'} embedded />
                        {/key}
                    </div>
                </div>

                <div class="card mb-3">
                    <div class="card-header flex-wrap gap-2">
                        <div class="me-auto">
                            <h3 class="card-title">Robots and labs</h3>
                            <p class="card-subtitle">
                                Each robot has its own calendar. Research threads get access from a lab admin on the Threads page;
                                "needs approval" also sends every booking to a lab admin.
                            </p>
                        </div>
                        <button type="button" class="btn btn-primary btn-sm" on:click={() => editResource(null)}>
                            <IconPlus size={16} class="icon" />Add robot
                        </button>
                    </div>
                    {#if resources.length === 0}
                        <div class="empty">
                            <p class="empty-title">No robots yet.</p>
                        </div>
                    {:else}
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr>
                                        <th>Name</th>
                                        <th>Lab</th>
                                        <th>Rules</th>
                                        <th class="w-1"></th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {#each resources as entry (entry.resource.ID)}
                                        {@const r = entry.resource}
                                        <tr class:text-secondary={!r.active}>
                                            <td>
                                                <div class="d-flex align-items-center gap-2">
                                                    <span class="avatar avatar-sm {r.lab === 'mocap' ? 'bg-pink-lt' : 'bg-orange-lt'}">
                                                        {#if r.lab === 'mocap'}<IconVideo size={18} />{:else}<IconRobot size={18} />{/if}
                                                    </span>
                                                    <div>
                                                        <div class="fw-medium">{r.name}</div>
                                                        <div class="small text-secondary">#{r.slug}</div>
                                                    </div>
                                                </div>
                                            </td>
                                            <td class="text-nowrap">
                                                {r.lab === 'mocap' ? 'Mocap Lab' : r.lab === 'mech' ? 'Mech Lab' : r.lab}
                                                {#if !r.active}<span class="badge bg-secondary-lt ms-1">not bookable</span>{/if}
                                            </td>
                                            <td>
                                                <div class="d-flex flex-wrap gap-1">
                                                    {#if r.requires_thread}<span class="badge bg-azure-lt">threads only</span>{/if}
                                                    {#if r.requires_approval}<span class="badge bg-yellow-lt">needs approval</span>{/if}
                                                    {#if r.max_hours}<span class="badge bg-secondary-lt">up to {r.max_hours} h</span>{/if}
                                                    {#if r.max_future_bookings}<span class="badge bg-secondary-lt">up to {r.max_future_bookings} upcoming</span>{/if}
                                                </div>
                                            </td>
                                            <td>
                                                <button type="button" class="btn btn-outline-secondary btn-sm" on:click={() => editResource(entry)}>
                                                    <IconPencil size={16} class="icon" />Edit
                                                </button>
                                            </td>
                                        </tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                    {/if}
                </div>

                <div class="card">
                    <div class="card-header flex-wrap gap-2">
                        <h3 class="card-title me-auto">All bookings</h3>
                        <div class="scroll-x">
                            <div class="nav nav-segmented nav-sm" role="tablist">
                                <button type="button" class="nav-link" role="tab" aria-selected={bookingFilter === 'upcoming'} class:active={bookingFilter === 'upcoming'} on:click={() => (bookingFilter = 'upcoming')}>Upcoming</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={bookingFilter === 'pending'} class:active={bookingFilter === 'pending'} on:click={() => (bookingFilter = 'pending')}>Pending</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={bookingFilter === 'no-show'} class:active={bookingFilter === 'no-show'} on:click={() => (bookingFilter = 'no-show')}>No-shows</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={bookingFilter === 'all'} class:active={bookingFilter === 'all'} on:click={() => (bookingFilter = 'all')}>All (from a week ago)</button>
                            </div>
                        </div>
                    </div>
                    {#if loading}
                        <div class="card-body text-secondary">Loading bookings...</div>
                    {:else if shownBookings.length === 0}
                        <div class="empty">
                            <p class="empty-title">No bookings{bookingFilter === 'all' ? '' : ' for this filter'}.</p>
                        </div>
                    {:else}
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr>
                                        <th>When</th>
                                        <th>Resource</th>
                                        <th>Purpose</th>
                                        <th>Booked by</th>
                                        <th>Status</th>
                                        <th class="w-1"></th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {#each shownBookings as booking (booking.ID)}
                                        <tr class:past={new Date(booking.end_time) < new Date()}>
                                            <td>{@render when(booking)}</td>
                                            <td>{@render bookingWhat(booking)}</td>
                                            <td class="purpose-cell">{booking.purpose}</td>
                                            <td class="text-nowrap">
                                                <div>{booking.booked_by}</div>
                                                <div class="small">{@render phone(booking.phone)}</div>
                                            </td>
                                            <td>
                                                <div class="d-flex flex-wrap gap-1">
                                                    {#if booking.status === 'pending'}<span class="badge bg-yellow-lt">awaiting approval</span>{/if}
                                                    {#if booking.reviewed_by}<span class="badge bg-secondary-lt">reviewed by {booking.reviewed_by}</span>{/if}
                                                    {#if booking.no_show}<span class="badge bg-red-lt">no-show{#if booking.no_show_reported_by}, reported by {booking.no_show_reported_by}{/if}</span>{/if}
                                                </div>
                                            </td>
                                            <td>
                                                <div class="btn-list flex-nowrap">
                                                    {#if booking.no_show}
                                                        <button type="button" class="btn btn-outline-secondary btn-sm text-nowrap" on:click={() => clearNoShow(booking)}>Clear no-show</button>
                                                    {/if}
                                                    <button type="button" class="btn btn-outline-danger btn-sm" on:click={() => deleteBooking(booking)}>
                                                        <IconTrash size={16} class="icon" />Delete
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Approvals (loan and booking approvers) -->
            {#if currentView === 'approvals'}
                <p class="text-secondary">Requests waiting for a decision. Someone else has to approve your own requests.</p>

                {#if can(adminInfo, 'approve_loans')}
                    <div class="card mb-3">
                        <div class="card-header">
                            <h3 class="card-title">
                                Loan requests
                                {#if pendingLoans.length}<span class="badge bg-red-lt ms-1">{pendingLoans.length}</span>{/if}
                            </h3>
                        </div>
                        {#if pendingLoans.length === 0}
                            <div class="empty">
                                <p class="empty-title">No loan requests waiting.</p>
                            </div>
                        {:else}
                            <div class="table-responsive">
                                <table class="table table-vcenter card-table">
                                    <thead>
                                        <tr>
                                            <th>Item</th>
                                            <th>Borrower</th>
                                            <th>Lab</th>
                                            <th>Qty</th>
                                            <th>Expected return</th>
                                            <th>Requested</th>
                                            <th class="w-1"></th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {#each pendingLoans as loan (loan.ID)}
                                            <tr>
                                                <td>
                                                    <div class="d-flex align-items-center gap-2">
                                                        {@render thumb(loan.photo_filename, loan.item_name)}
                                                        <span class="fw-medium">{loan.item_name}</span>
                                                    </div>
                                                </td>
                                                <td class="text-nowrap">{loan.borrower_name}{@render acctBadge(loan)}</td>
                                                <td class="text-nowrap">{loan.lab_location}</td>
                                                <td class="tabular">{loan.quantity_borrowed}</td>
                                                <td class="text-nowrap">{formatExpectedReturn(loan.expected_return_date)}</td>
                                                <td class="text-nowrap">{formatDate(loan.CreatedAt)}</td>
                                                <td>
                                                    <div class="btn-list flex-nowrap">
                                                        {#if loan.user_id === adminInfo.id}
                                                            <span class="small text-secondary">Your own request: another approver has to decide it.</span>
                                                        {:else}
                                                            <button type="button" class="btn btn-success btn-sm" on:click={() => decideLoan(loan, 'approve')} disabled={deciding === 'loan' + loan.ID}>
                                                                <IconCheck size={16} class="icon" />Approve
                                                            </button>
                                                        {/if}
                                                        <button type="button" class="btn btn-outline-danger btn-sm" on:click={() => decideLoan(loan, 'reject')} disabled={deciding === 'loan' + loan.ID}>
                                                            <IconX size={16} class="icon" />Decline
                                                        </button>
                                                    </div>
                                                </td>
                                            </tr>
                                        {/each}
                                    </tbody>
                                </table>
                            </div>
                        {/if}
                    </div>
                {/if}

                {#if can(adminInfo, 'approve_mech_bookings')}
                    <div class="card">
                        <div class="card-header">
                            <h3 class="card-title">
                                Booking requests
                                {#if pendingBookings.length}<span class="badge bg-red-lt ms-1">{pendingBookings.length}</span>{/if}
                            </h3>
                        </div>
                        {#if pendingBookings.length === 0}
                            <div class="empty">
                                <p class="empty-title">No booking requests waiting.</p>
                            </div>
                        {:else}
                            <div class="table-responsive">
                                <table class="table table-vcenter card-table">
                                    <thead>
                                        <tr>
                                            <th>When</th>
                                            <th>Resource</th>
                                            <th>Purpose</th>
                                            <th>Requested by</th>
                                            <th class="w-1"></th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {#each pendingBookings as booking (booking.ID)}
                                            <tr>
                                                <td>{@render when(booking)}</td>
                                                <td>{@render bookingWhat(booking)}</td>
                                                <td class="purpose-cell">{booking.purpose}</td>
                                                <td class="text-nowrap">{booking.booked_by}</td>
                                                <td>
                                                    <div class="btn-list flex-nowrap">
                                                        {#if booking.user_id === adminInfo.id}
                                                            <span class="small text-secondary text-nowrap">Your own request</span>
                                                        {:else}
                                                            <button type="button" class="btn btn-success btn-sm" on:click={() => decideBooking(booking, 'approve')} disabled={deciding === 'booking' + booking.ID}>
                                                                <IconCheck size={16} class="icon" />Approve
                                                            </button>
                                                        {/if}
                                                        <button type="button" class="btn btn-outline-danger btn-sm" on:click={() => decideBooking(booking, 'reject')} disabled={deciding === 'booking' + booking.ID}>
                                                            <IconX size={16} class="icon" />Decline
                                                        </button>
                                                    </div>
                                                </td>
                                            </tr>
                                        {/each}
                                    </tbody>
                                </table>
                            </div>
                        {/if}
                    </div>
                {/if}
            {/if}

            <!-- Item history -->
            {#if currentView === 'history'}
                <div class="card">
                    <div class="card-header">
                        <div>
                            <h3 class="card-title">Item history</h3>
                            <p class="card-subtitle">Every loan in order: borrowed, returned and missing</p>
                        </div>
                    </div>
                    <div class="card-body border-bottom">
                        <label class="form-label" for="history-search">Search by name, item, phone, lab, purpose, status or admin</label>
                        <div class="input-group mb-2">
                            <span class="input-group-text"><IconSearch size={18} /></span>
                            <input type="text" id="history-search" class="form-control" bind:value={historySearchQuery} placeholder="Search all history records..." autocomplete="off" />
                            {#if historySearchQuery && historySearchQuery.trim()}
                                <button class="btn btn-outline-secondary" on:click={clearHistorySearch} title="Clear search" aria-label="Clear search" type="button"><IconX size={18} /></button>
                            {/if}
                        </div>
                        <div class="scroll-x">
                            <div class="nav nav-segmented nav-sm" role="tablist">
                                <button type="button" class="nav-link" role="tab" aria-selected={historyStatus === 'all'} class:active={historyStatus === 'all'} on:click={() => (historyStatus = 'all')}>All</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={historyStatus === 'active'} class:active={historyStatus === 'active'} on:click={() => (historyStatus = 'active')}>Borrowed</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={historyStatus === 'overdue'} class:active={historyStatus === 'overdue'} on:click={() => (historyStatus = 'overdue')}>Overdue</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={historyStatus === 'returned'} class:active={historyStatus === 'returned'} on:click={() => (historyStatus = 'returned')}>Returned</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={historyStatus === 'not_found'} class:active={historyStatus === 'not_found'} on:click={() => (historyStatus = 'not_found')}>Missing</button>
                            </div>
                        </div>
                        {#if historySearchQuery.trim() || historyStatus !== 'all'}
                            <div class="small text-secondary mt-2">Showing {filteredHistoryItems.length} of {historyItems.length} history records</div>
                        {/if}
                    </div>

                    {#if loading}
                        <div class="card-body text-secondary">Loading item history...</div>
                    {:else if historyItems.length === 0}
                        <div class="empty">
                            <p class="empty-title">No history found.</p>
                        </div>
                    {:else if filteredHistoryItems.length === 0}
                        <div class="empty">
                            <p class="empty-title">No matches</p>
                            <p class="empty-subtitle text-secondary">
                                No history records match{historySearchQuery.trim() ? ` your search for "${historySearchQuery}"` : ' this filter'}.
                            </p>
                            <div class="empty-action">
                                <button type="button" class="btn btn-outline-primary" on:click={(e) => { historyStatus = 'all'; clearHistorySearch(e); }}>Clear search</button>
                            </div>
                        </div>
                    {:else}
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr>
                                        <th>Item</th>
                                        <th>Status</th>
                                        <th>Date</th>
                                        <th>Borrower</th>
                                        <th>Phone</th>
                                        <th>Lab</th>
                                        <th>Qty</th>
                                        <th>Purpose</th>
                                        <th>Last handled by</th>
                                        <th>Requested</th>
                                        <th>Expected return</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {#each filteredHistoryItems as item}
                                        {@const late = isOverdue(item.expected_return_date)}
                                        <tr>
                                            <td>
                                                <div class="d-flex align-items-center gap-2">
                                                    {@render thumb(item.photo_filename, item.item_name)}
                                                    <span class="fw-medium">{item.item_name}</span>
                                                </div>
                                            </td>
                                            <td>
                                                <div class="d-flex flex-wrap gap-1">
                                                    {#if item.status === 'returned'}<span class="badge bg-green-lt">Returned</span>
                                                    {:else if item.status === 'not_found'}<span class="badge bg-red-lt">Missing</span>
                                                    {:else if item.status === 'active'}<span class="badge bg-blue-lt">Borrowed</span>
                                                    {:else}<span class="badge bg-secondary-lt">{item.status.charAt(0).toUpperCase() + item.status.slice(1)}</span>{/if}
                                                    {#if item.status === 'active' && late}
                                                        <span class="badge bg-red">Overdue</span>
                                                    {:else if item.status === 'returned' && late}
                                                        <span class="badge bg-yellow-lt" title="Returned overdue">Late</span>
                                                    {/if}
                                                </div>
                                            </td>
                                            <td class="text-nowrap">
                                                <div class="small text-secondary">
                                                    {#if item.status === 'returned'}Returned{:else if item.status === 'not_found'}Marked missing{:else}Borrowed{/if}
                                                </div>
                                                {#if item.status === 'returned' && item.returned_at}{formatDate(item.returned_at)}
                                                {:else if item.status === 'not_found' && item.approved_at}{formatDate(item.approved_at)}
                                                {:else}{formatDate(item.CreatedAt)}{/if}
                                            </td>
                                            <td class="text-nowrap">{item.borrower_name}{@render acctBadge(item)}</td>
                                            <td>{@render phone(item.borrower_phone)}</td>
                                            <td class="text-nowrap">{item.lab_location}</td>
                                            <td class="tabular">{item.quantity_borrowed}</td>
                                            <td class="purpose-cell">{item.purpose || '-'}</td>
                                            <td class="text-nowrap">{item.approved_by || '-'}</td>
                                            <td class="text-nowrap">{formatDate(item.CreatedAt)}</td>
                                            <td class="text-nowrap">
                                                {#if item.expected_return_date}
                                                    <span class:text-danger={item.status === 'active' && late}>{formatExpectedReturn(item.expected_return_date)}</span>
                                                {:else}
                                                    <span class="text-secondary">-</span>
                                                {/if}
                                            </td>
                                        </tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- One lab's loans -->
            {#if currentView === 'lab-view'}
                <div class="card mb-3">
                    <div class="card-header">
                        <div>
                            <h3 class="card-title">{selectedLab}: borrowed items</h3>
                            <p class="card-subtitle">
                                Items returned more than 2 weeks ago are archived automatically. See them under "Archived", or everything in Item history.
                            </p>
                        </div>
                    </div>
                    <div class="card-body">
                        <div class="scroll-x">
                            <div class="nav nav-segmented" role="tablist">
                                <button type="button" class="nav-link" role="tab" aria-selected={selectedFilter === 'all'} class:active={selectedFilter === 'all'} on:click={() => filterLabLoans('all')}>All</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={selectedFilter === 'borrowed'} class:active={selectedFilter === 'borrowed'} on:click={() => filterLabLoans('borrowed')}>Borrowed</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={selectedFilter === 'returned'} class:active={selectedFilter === 'returned'} on:click={() => filterLabLoans('returned')}>Returned</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={selectedFilter === 'not_found'} class:active={selectedFilter === 'not_found'} on:click={() => filterLabLoans('not_found')}>Missing</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={selectedFilter === 'archived'} class:active={selectedFilter === 'archived'} on:click={() => filterLabLoans('archived')}>
                                    <IconArchive size={16} class="icon me-1" />Archived
                                </button>
                            </div>
                        </div>
                    </div>
                </div>

                {#if loading}
                    <p class="text-secondary">Loading lab items...</p>
                {:else if labLoans.length === 0}
                    <div class="card">
                        <div class="empty">
                            <p class="empty-title">No items found for the selected filter.</p>
                        </div>
                    </div>
                {:else}
                    <div class="row row-cards g-3">
                        {#each labLoans as loan}
                            {@const overdue = isOverdue(loan.expected_return_date) && loan.status === 'active'}
                            <div class="col-12 col-md-6 col-xxl-4">
                                <div class="card h-100">
                                    {#if overdue}<div class="card-status-top bg-danger"></div>
                                    {:else if loan.status === 'not_found'}<div class="card-status-top bg-warning"></div>{/if}
                                    <div class="card-body">
                                        <div class="d-flex align-items-start gap-2 mb-3">
                                            {@render thumb(loan.photo_filename, loan.item_name)}
                                            <h3 class="card-title mb-0 flex-fill">{loan.item_name}</h3>
                                            <div class="d-flex flex-wrap justify-content-end gap-1">
                                                {#if selectedFilter === 'archived'}
                                                    <span class="badge bg-secondary-lt">Archived</span>
                                                {:else if loan.status === 'not_found'}
                                                    <span class="badge bg-red-lt">Missing</span>
                                                {:else if loan.status === 'returned'}
                                                    <span class="badge bg-green-lt">Returned</span>
                                                {:else}
                                                    <span class="badge bg-blue-lt">Borrowed</span>
                                                {/if}
                                                {#if overdue}<span class="badge bg-red">Overdue</span>{/if}
                                            </div>
                                        </div>
                                        <div class="datagrid">
                                            <div class="datagrid-item">
                                                <div class="datagrid-title">Borrower</div>
                                                <div class="datagrid-content">{loan.borrower_name}{@render acctBadge(loan)}</div>
                                            </div>
                                            <div class="datagrid-item">
                                                <div class="datagrid-title">Phone</div>
                                                <div class="datagrid-content">{@render phone(loan.borrower_phone)}</div>
                                            </div>
                                            <div class="datagrid-item">
                                                <div class="datagrid-title">Quantity</div>
                                                <div class="datagrid-content tabular">{loan.quantity_borrowed}</div>
                                            </div>
                                            <div class="datagrid-item">
                                                <div class="datagrid-title">Purpose</div>
                                                <div class="datagrid-content">{loan.purpose || '-'}</div>
                                            </div>
                                            <div class="datagrid-item">
                                                <div class="datagrid-title">Expected return</div>
                                                <div class="datagrid-content" class:text-danger={overdue}>{formatExpectedReturn(loan.expected_return_date)}</div>
                                            </div>
                                            <div class="datagrid-item">
                                                <div class="datagrid-title">Borrowed</div>
                                                <div class="datagrid-content">{formatDate(loan.CreatedAt)}</div>
                                            </div>
                                            {#if loan.approved_by}
                                                <div class="datagrid-item">
                                                    <div class="datagrid-title">Last handled by</div>
                                                    <div class="datagrid-content">{loan.approved_by}</div>
                                                </div>
                                            {/if}
                                        </div>
                                    </div>
                                    {#if loan.status === 'active'}
                                        <div class="card-footer">
                                            <div class="form-label">Extend by</div>
                                            <div class="row g-2 mb-2">
                                                <div class="col-6">
                                                    <div class="input-group">
                                                        <input class="form-control" type="number" id="extend-days-{loan.ID}" aria-label="Extend by days" min="0" max="365" placeholder="Days" value="0" />
                                                        <span class="input-group-text">days</span>
                                                    </div>
                                                </div>
                                                <div class="col-6">
                                                    <div class="input-group">
                                                        <input class="form-control" type="number" id="extend-hours-{loan.ID}" aria-label="Extend by hours" min="0" max="23" placeholder="Hours" value="0" />
                                                        <span class="input-group-text">hours</span>
                                                    </div>
                                                </div>
                                            </div>
                                            <div class="btn-list">
                                                <button
                                                    type="button"
                                                    class="btn btn-primary"
                                                    on:click={() => {
                                                        const days = document.getElementById(`extend-days-${loan.ID}`).value;
                                                        const hours = document.getElementById(`extend-hours-${loan.ID}`).value;
                                                        extendLoan(loan.ID, days, hours);
                                                    }}
                                                    disabled={loading}
                                                >
                                                    <IconCalendar size={18} class="icon" />Extend
                                                </button>
                                                <button type="button" class="btn btn-outline-warning" on:click={() => markAsMissing(loan.ID, loan.item_name)} disabled={loading}>
                                                    <IconSearch size={18} class="icon" />Can't find this item
                                                </button>
                                            </div>
                                        </div>
                                    {/if}
                                    {#if loan.status === 'returned'}
                                        <div class="card-footer">
                                            <div class="d-flex flex-wrap align-items-center gap-2">
                                                <span class="text-success"><IconCircleCheck size={18} class="icon" /> Returned{#if loan.returned_at} on {formatDate(loan.returned_at)}{/if}</span>
                                                {#if isOverdue(loan.expected_return_date)}
                                                    <span class="badge bg-yellow-lt">Returned overdue</span>
                                                {/if}
                                            </div>
                                        </div>
                                    {:else if loan.status === 'not_found'}
                                        <div class="card-footer d-flex flex-wrap align-items-center justify-content-between gap-2">
                                            <span class="text-secondary">Marked as missing</span>
                                            <button type="button" class="btn btn-outline-success btn-sm" on:click={() => markAsFound(loan.ID)} disabled={loading}>
                                                <IconCheck size={16} class="icon" />Mark as found
                                            </button>
                                        </div>
                                    {/if}
                                </div>
                            </div>
                        {/each}
                    </div>
                {/if}
            {/if}

            <!-- Item catalogue -->
            {#if currentView === 'items'}
                <div class="card">
                    <div class="card-header">
                        <div>
                            <h3 class="card-title">Item catalogue</h3>
                            <p class="card-subtitle">
                                People add items as they borrow. Here you tidy up: merge duplicates, mark valuable items as tracked
                                (their custodian confirms them monthly), set rules.
                            </p>
                        </div>
                    </div>
                    <div class="card-body border-bottom">
                        <label class="form-label" for="item-search">Search items by name</label>
                        <div class="d-flex gap-2 mb-2">
                            <div class="input-group">
                                <span class="input-group-text"><IconSearch size={18} /></span>
                                <input type="text" id="item-search" class="form-control" bind:value={itemSearch} placeholder="Search the catalogue..." autocomplete="off" />
                                {#if itemSearch.trim()}
                                    <button class="btn btn-outline-secondary" on:click={() => (itemSearch = '')} title="Clear search" aria-label="Clear search" type="button"><IconX size={18} /></button>
                                {/if}
                            </div>
                            <button type="button" class="btn btn-outline-secondary text-nowrap" on:click={loadCatalogue}>
                                <IconRefresh size={18} class="icon" /><span class="d-none d-sm-inline">Refresh</span>
                            </button>
                        </div>
                        <div class="scroll-x">
                            <div class="nav nav-segmented nav-sm" role="tablist">
                                <button type="button" class="nav-link" role="tab" aria-selected={itemFilter === 'all'} class:active={itemFilter === 'all'} on:click={() => (itemFilter = 'all')}>All</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={itemFilter === 'out'} class:active={itemFilter === 'out'} on:click={() => (itemFilter = 'out')}>Out now</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={itemFilter === 'issue'} class:active={itemFilter === 'issue'} on:click={() => (itemFilter = 'issue')}>With issues</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={itemFilter === 'tracked'} class:active={itemFilter === 'tracked'} on:click={() => (itemFilter = 'tracked')}>Tracked</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={itemFilter === 'approval'} class:active={itemFilter === 'approval'} on:click={() => (itemFilter = 'approval')}>Needs approval</button>
                                <button type="button" class="nav-link" role="tab" aria-selected={itemFilter === 'lab_only'} class:active={itemFilter === 'lab_only'} on:click={() => (itemFilter = 'lab_only')}>Lab only</button>
                            </div>
                        </div>
                        <div class="small text-secondary mt-2">Showing {shownItems.length} of {catalogue.length} items</div>
                    </div>

                    {#if shownItems.length === 0}
                        <div class="empty">
                            <p class="empty-title">No items{itemSearch.trim() ? ` match "${itemSearch}"` : ' for this filter'}.</p>
                        </div>
                    {:else}
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr>
                                        <th>Item</th>
                                        <th>Lab</th>
                                        <th>Out now</th>
                                        <th>Custodian</th>
                                        <th>Issue</th>
                                        <th class="w-1"></th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {#each shownItems as item (item.ID)}
                                        {@const out = itemsOut(item)}
                                        <tr>
                                            <td class="item-cell">
                                                <div class="fw-medium">{item.name}</div>
                                                <div class="d-flex flex-wrap gap-1 mt-1">
                                                    {#if out > 0}<span class="badge bg-blue-lt">{out} out</span>{/if}
                                                    {#if item.tracked}<span class="badge bg-green-lt">Tracked</span>{/if}
                                                    {#if item.needs_approval}<span class="badge bg-yellow-lt">Needs approval</span>{/if}
                                                    {#if item.lab_only}<span class="badge bg-secondary-lt">Lab only</span>{/if}
                                                </div>
                                                {#if item.notes}<div class="small text-secondary mt-1">{item.notes}</div>{/if}
                                            </td>
                                            <td class="text-nowrap">{item.home_lab || '-'}</td>
                                            <td class="text-nowrap tabular">{out}{#if item.total_quantity} of {item.total_quantity}{/if}</td>
                                            <td class="text-nowrap">
                                                {#if item.tracked}
                                                    <div>{personName(item.custodian_id)}</div>
                                                    {#if item.last_verified_at}<div class="small text-secondary">Last confirmed {formatDate(item.last_verified_at)}</div>{/if}
                                                {:else}
                                                    <span class="text-secondary">-</span>
                                                {/if}
                                            </td>
                                            <td class="issue-cell">
                                                {#if item.issue}
                                                    <div class="text-warning"><IconAlert size={16} class="icon" /> {item.issue}</div>
                                                    {#if item.issue_reported_by}<div class="small text-secondary">Reported by {item.issue_reported_by}</div>{/if}
                                                    <button type="button" class="btn btn-outline-success btn-sm mt-1" on:click={() => clearIssue(item)}>
                                                        <IconCheck size={16} class="icon" />Clear issue
                                                    </button>
                                                {:else}
                                                    <span class="text-secondary">-</span>
                                                {/if}
                                            </td>
                                            <td>
                                                <div class="btn-list flex-nowrap">
                                                    <button type="button" class="btn btn-outline-secondary btn-sm" on:click={() => (editingItem = { ...item, notes: item.notes || '', custodian_id: item.custodian_id ?? null })}>
                                                        <IconPencil size={16} class="icon" />Edit
                                                    </button>
                                                    <div class="input-group input-group-sm flex-nowrap merge-group">
                                                        <select class="form-select" bind:value={mergeInto[item.ID]} aria-label="Merge {item.name} into">
                                                            <option value={undefined}>Merge into...</option>
                                                            {#each catalogue as other (other.ID)}
                                                                {#if other.ID !== item.ID}<option value={other.ID}>{other.name}</option>{/if}
                                                            {/each}
                                                        </select>
                                                        <button type="button" class="btn btn-outline-secondary" disabled={!mergeInto[item.ID]} on:click={() => mergeItem(item)}>
                                                            <IconMerge size={16} class="icon" />Merge
                                                        </button>
                                                    </div>
                                                    <button type="button" class="btn btn-outline-danger btn-sm" on:click={() => deleteItem(item)}>
                                                        <IconTrash size={16} class="icon" />Delete
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Change password -->
            {#if currentView === 'change-password'}
                <div class="row">
                    <div class="col-12 col-xl-8">
                        <form class="card" on:submit|preventDefault={changePassword}>
                            <div class="card-header">
                                <div>
                                    <h3 class="card-title">Change password</h3>
                                    <p class="card-subtitle">Changing it signs you out everywhere else; this browser stays signed in.</p>
                                </div>
                            </div>
                            <div class="card-body">
                                <div class="mb-3">
                                    <label class="form-label" for="old_password">Current password</label>
                                    <input class="form-control" type="password" id="old_password" bind:value={changePasswordForm.old_password} required autocomplete="current-password" placeholder="Enter current password" />
                                </div>
                                <div class="mb-3">
                                    <label class="form-label" for="new_password">New password</label>
                                    <input class="form-control" type="password" id="new_password" bind:value={changePasswordForm.new_password} required minlength="8" maxlength="72" autocomplete="new-password" placeholder="At least 8 characters" />
                                </div>
                                <div>
                                    <label class="form-label" for="confirm_password">Confirm new password</label>
                                    <input class="form-control" type="password" id="confirm_password" bind:value={changePasswordForm.confirm_password} required minlength="8" maxlength="72" autocomplete="new-password" placeholder="Confirm new password" />
                                </div>
                            </div>
                            <div class="card-footer">
                                <div class="btn-list justify-content-end">
                                    <button type="button" class="btn btn-outline-secondary" on:click={goToDashboard}>Cancel</button>
                                    <button type="submit" class="btn btn-primary" disabled={savingPassword}>{savingPassword ? 'Changing...' : 'Change password'}</button>
                                </div>
                            </div>
                        </form>
                    </div>
                </div>
            {/if}

            <!-- Users & groups (server admins) -->
            {#if currentView === 'users' && can(adminInfo, 'manage_users')}
                <!-- Sign-ups waiting -->
                <div class="card mb-3">
                    {#if pendingUsers.length > 0}<div class="card-status-top bg-warning"></div>{/if}
                    <div class="card-header">
                        <h3 class="card-title me-auto">
                            <IconHourglass size={18} class="icon text-secondary" />
                            Waiting for approval
                            {#if pendingUsers.length}<span class="badge bg-red-lt ms-1">{pendingUsers.length}</span>{/if}
                        </h3>
                        <button type="button" class="btn btn-outline-secondary btn-sm" on:click={loadUsers}>
                            <IconRefresh size={16} class="icon" />Refresh
                        </button>
                    </div>
                    {#if pendingUsers.length === 0}
                        <div class="card-body text-secondary">Nobody is waiting. New sign-ups appear here.</div>
                    {:else}
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr>
                                        <th>Name</th>
                                        <th>Phone</th>
                                        <th>Signed up</th>
                                        <th>Group</th>
                                        <th class="w-1"></th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {#each pendingUsers as user (user.id)}
                                        <tr>
                                            <td>
                                                <div class="fw-medium">{user.name}</div>
                                                <div class="small text-secondary">{user.email}</div>
                                            </td>
                                            <td class="text-nowrap tabular">{user.phone}</td>
                                            <td class="text-nowrap">{new Date(user.created_at).toLocaleDateString()}</td>
                                            <td>
                                                <select class="form-select form-select-sm group-select" value={user.group_id} on:change={(e) => changeGroup(e, user)} aria-label="Group for {user.name}">
                                                    {#each groups as g}<option value={g.id}>{g.name}</option>{/each}
                                                </select>
                                            </td>
                                            <td>
                                                <div class="btn-list flex-nowrap">
                                                    <button type="button" class="btn btn-success btn-sm" on:click={() => updateUser(user, { status: 'active' }, `${user.name} approved`)}>
                                                        <IconCheck size={16} class="icon" />Approve
                                                    </button>
                                                    <button type="button" class="btn btn-outline-danger btn-sm" on:click={() => confirm(`Reject ${user.name}'s sign-up?`) && updateUser(user, { status: 'disabled' }, `${user.name} rejected`)}>
                                                        <IconX size={16} class="icon" />Reject
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                    {/if}
                </div>

                <!-- Everyone -->
                <div class="card mb-3">
                    <div class="card-header">
                        <div>
                            <h3 class="card-title">Current users</h3>
                            <p class="card-subtitle">{activeUserCount} active, {disabledUserCount} disabled</p>
                        </div>
                    </div>
                    <div class="card-body border-bottom">
                        <div class="row g-2 align-items-center">
                            <div class="col-12 col-md">
                                <div class="input-group">
                                    <span class="input-group-text"><IconSearch size={18} /></span>
                                    <input type="search" class="form-control" bind:value={userSearch} placeholder="Search name, email, phone or group" aria-label="Search users" />
                                </div>
                            </div>
                            <div class="col-12 col-md-auto scroll-x">
                                <div class="nav nav-segmented nav-sm" role="tablist">
                                    <button type="button" class="nav-link" role="tab" aria-selected={userFilter === 'all'} class:active={userFilter === 'all'} on:click={() => (userFilter = 'all')}>All</button>
                                    <button type="button" class="nav-link" role="tab" aria-selected={userFilter === 'active'} class:active={userFilter === 'active'} on:click={() => (userFilter = 'active')}>Active</button>
                                    <button type="button" class="nav-link" role="tab" aria-selected={userFilter === 'disabled'} class:active={userFilter === 'disabled'} on:click={() => (userFilter = 'disabled')}>Disabled</button>
                                    <button type="button" class="nav-link" role="tab" aria-selected={userFilter === 'extra'} class:active={userFilter === 'extra'} on:click={() => (userFilter = 'extra')}>Extra access</button>
                                </div>
                            </div>
                        </div>
                    </div>

                    {#if shownUsers.length === 0}
                        <div class="empty">
                            <p class="empty-title">No users match.</p>
                            <div class="empty-action">
                                <button type="button" class="btn btn-outline-primary" on:click={() => { userSearch = ''; userFilter = 'all'; }}>Show everyone</button>
                            </div>
                        </div>
                    {:else}
                        <div class="table-responsive">
                            <table class="table table-vcenter card-table">
                                <thead>
                                    <tr>
                                        <th>User</th>
                                        <th>Phone</th>
                                        <th>Group</th>
                                        <th>Status</th>
                                        <th>Extra access</th>
                                        <th>Joined</th>
                                        <th class="w-1"></th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {#each shownUsers as user (user.id)}
                                        <tr class:text-secondary={user.status === 'disabled'}>
                                            <td>
                                                <div class="fw-medium">
                                                    {user.name}
                                                    {#if user.id === adminInfo.id}<span class="badge bg-primary-lt ms-1">You</span>{/if}
                                                </div>
                                                <div class="small text-secondary">{user.email}</div>
                                            </td>
                                            <td class="text-nowrap tabular">{user.phone || '-'}</td>
                                            <td>
                                                {#if user.id === adminInfo.id}
                                                    <span class="badge bg-secondary-lt">{groupName(user.group_id)}</span>
                                                {:else}
                                                    <select class="form-select form-select-sm group-select" value={user.group_id} on:change={(e) => changeGroup(e, user)} aria-label="Group for {user.name}">
                                                        {#each groups as g}<option value={g.id}>{g.name}</option>{/each}
                                                    </select>
                                                {/if}
                                            </td>
                                            <td>
                                                <span class="badge {user.status === 'active' ? 'bg-green-lt' : user.status === 'pending' ? 'bg-yellow-lt' : 'bg-red-lt'}">{user.status}</span>
                                            </td>
                                            <td>
                                                {#if user.overrides && user.overrides.length > 0}
                                                    <div class="d-flex flex-wrap gap-1">
                                                        {#each user.overrides as o}
                                                            <span class="badge {o.allow ? 'bg-green-lt' : 'bg-red-lt'}">{o.allow ? '+' : '-'} {permLabel(o.perm)}</span>
                                                        {/each}
                                                    </div>
                                                {:else}
                                                    <span class="text-secondary">-</span>
                                                {/if}
                                            </td>
                                            <td class="text-nowrap">{user.created_at ? new Date(user.created_at).toLocaleDateString() : '-'}</td>
                                            <td>
                                                <div class="btn-list flex-nowrap">
                                                    {#if user.id !== adminInfo.id}
                                                        <button type="button" class="btn btn-outline-secondary btn-sm" on:click={() => (editingUser && editingUser.id === user.id ? (editingUser = null) : editOverrides(user))}>
                                                            <IconShieldLock size={16} class="icon" />Access
                                                        </button>
                                                        <button type="button" class="btn btn-ghost-secondary btn-sm text-nowrap" on:click={() => resetPassword(user)}>
                                                            <IconKey size={16} class="icon" />Reset password
                                                        </button>
                                                        {#if user.status === 'active'}
                                                            <button type="button" class="btn btn-outline-danger btn-sm" on:click={() => confirm(`Disable ${user.name}? They will be logged out.`) && updateUser(user, { status: 'disabled' }, `${user.name} disabled`)}>
                                                                <IconBan size={16} class="icon" />Disable
                                                            </button>
                                                        {:else}
                                                            <button type="button" class="btn btn-outline-success btn-sm" on:click={() => updateUser(user, { status: 'active' }, `${user.name} enabled`)}>
                                                                <IconCheck size={16} class="icon" />Enable
                                                            </button>
                                                        {/if}
                                                    {:else}
                                                        <button type="button" class="btn btn-ghost-secondary btn-sm text-nowrap" on:click={showChangePasswordView}>
                                                            <IconKey size={16} class="icon" />Change my password
                                                        </button>
                                                    {/if}
                                                </div>
                                            </td>
                                        </tr>
                                    {/each}
                                </tbody>
                            </table>
                        </div>
                    {/if}
                </div>

                <!-- What each group may do -->
                <div class="card mb-3">
                    <div class="card-header">
                        <div>
                            <h3 class="card-title">Group permissions</h3>
                            <p class="card-subtitle">What each group can do by default. Changes apply immediately to everyone in the group.</p>
                        </div>
                    </div>
                    <div class="table-responsive">
                        <table class="table table-vcenter card-table">
                            <thead>
                                <tr>
                                    <th scope="col">Permission</th>
                                    {#each groups as g}
                                        <th scope="col" class="text-center">
                                            <div>{g.name}</div>
                                            <div class="small text-secondary text-lowercase fw-normal">{users.filter((u) => u.group_id === g.id).length} people</div>
                                        </th>
                                    {/each}
                                </tr>
                            </thead>
                            <tbody>
                                {#each allPermissions as perm}
                                    <tr>
                                        <th scope="row" class="fw-normal">{permLabel(perm)}</th>
                                        {#each groups as g}
                                            <td class="text-center">
                                                <input class="form-check-input m-0" type="checkbox" checked={g.permissions.includes(perm)} on:change={(e) => toggleGroupPerm(e, g, perm)} aria-label="{g.name}: {permLabel(perm)}" />
                                            </td>
                                        {/each}
                                    </tr>
                                {/each}
                            </tbody>
                        </table>
                    </div>
                </div>

                <!-- Danger zone -->
                <div class="card border-danger">
                    <div class="card-header">
                        <h3 class="card-title text-danger"><IconAlert size={18} class="icon" /> Danger zone</h3>
                    </div>
                    <div class="card-body d-flex flex-wrap align-items-center justify-content-between gap-3">
                        <div>
                            <div class="fw-medium">Delete all item data</div>
                            <div class="text-secondary">Permanently delete all inventory items. This cannot be undone.</div>
                        </div>
                        <button type="button" class="btn btn-danger" on:click={deleteAllItems}>
                            <IconTrash size={18} class="icon" />Delete all items
                        </button>
                    </div>
                </div>
            {/if}
        </div>
    </div>
{/if}

<svelte:window
    on:keydown={(e) => {
        if (e.key !== 'Escape') return;
        editingItem = null;
        editingResource = null;
        editingUser = null;
    }}
/>

<!-- Edit an item -->
{#if editingItem}
    <div class="modal modal-blur d-block" tabindex="-1" role="dialog" aria-modal="true" aria-labelledby="item-modal-title">
        <div class="modal-dialog modal-dialog-centered" role="document">
            <form class="modal-content" on:submit|preventDefault={saveItem}>
                <div class="modal-header">
                    <h5 class="modal-title" id="item-modal-title">Edit item</h5>
                    <button type="button" class="btn-close" aria-label="Close" on:click={() => (editingItem = null)}></button>
                </div>
                <div class="modal-body">
                    <div class="mb-3">
                        <label class="form-label" for="item-name-{editingItem.ID}">Name</label>
                        <input class="form-control" id="item-name-{editingItem.ID}" type="text" bind:value={editingItem.name} required />
                    </div>
                    <div class="row g-3 mb-3">
                        <div class="col-12 col-sm-6">
                            <label class="form-label" for="item-lab-{editingItem.ID}">Lab</label>
                            <select class="form-select" id="item-lab-{editingItem.ID}" bind:value={editingItem.home_lab}>
                                {#each labs as lab}<option value={lab}>{lab}</option>{/each}
                                {#if editingItem.home_lab && !labs.includes(editingItem.home_lab)}
                                    <option value={editingItem.home_lab}>{editingItem.home_lab}</option>
                                {/if}
                            </select>
                        </div>
                        <div class="col-12 col-sm-6">
                            <label class="form-label" for="item-qty-{editingItem.ID}">Total quantity</label>
                            <input class="form-control" id="item-qty-{editingItem.ID}" type="number" min="0" bind:value={editingItem.total_quantity} />
                            <div class="form-hint">0 means unknown</div>
                        </div>
                    </div>
                    <div class="mb-3">
                        <label class="form-label" for="item-notes-{editingItem.ID}">Notes</label>
                        <input class="form-control" id="item-notes-{editingItem.ID}" type="text" maxlength="500" bind:value={editingItem.notes} />
                    </div>
                    <div class="mb-3">
                        <label class="form-check">
                            <input class="form-check-input" type="checkbox" bind:checked={editingItem.tracked} />
                            <span class="form-check-label">Tracked (custodian confirms it monthly)</span>
                        </label>
                        <label class="form-check">
                            <input class="form-check-input" type="checkbox" bind:checked={editingItem.lab_only} />
                            <span class="form-check-label">Lab only (can't leave the lab)</span>
                        </label>
                        <label class="form-check mb-0">
                            <input class="form-check-input" type="checkbox" bind:checked={editingItem.needs_approval} />
                            <span class="form-check-label">Borrowing needs approval</span>
                        </label>
                    </div>
                    {#if editingItem.tracked}
                        <div>
                            <label class="form-label" for="item-cust-{editingItem.ID}">Custodian</label>
                            <select class="form-select" id="item-cust-{editingItem.ID}" bind:value={editingItem.custodian_id}>
                                <option value={null}>None</option>
                                {#each people as person}<option value={person.id}>{person.name}</option>{/each}
                            </select>
                        </div>
                    {/if}
                </div>
                <div class="modal-footer">
                    <button type="button" class="btn btn-outline-secondary" on:click={() => (editingItem = null)}>Cancel</button>
                    <button type="submit" class="btn btn-primary">Save</button>
                </div>
            </form>
        </div>
    </div>
    <div class="modal-backdrop fade show"></div>
{/if}

<!-- Add or edit a robot -->
{#if editingResource}
    <div class="modal modal-blur d-block" tabindex="-1" role="dialog" aria-modal="true" aria-labelledby="res-modal-title">
        <div class="modal-dialog modal-dialog-centered" role="document">
            <form class="modal-content" on:submit|preventDefault={saveResource}>
                <div class="modal-header">
                    <h5 class="modal-title" id="res-modal-title">{editingResource.ID ? 'Edit robot' : 'New robot'}</h5>
                    <button type="button" class="btn-close" aria-label="Close" on:click={() => (editingResource = null)}></button>
                </div>
                <div class="modal-body">
                    <div class="mb-3">
                        <label class="form-label" for="res-name">Name</label>
                        <input class="form-control" id="res-name" type="text" bind:value={editingResource.name} required placeholder="UR5 #1" />
                    </div>
                    {#if !editingResource.ID}
                        <div class="mb-3">
                            <label class="form-label" for="res-slug">Id</label>
                            <input class="form-control" id="res-slug" type="text" bind:value={editingResource.slug} required pattern="[a-z0-9][a-z0-9-]*" placeholder="ur5-1" />
                            <div class="form-hint">Lower-case letters, digits and dashes. It can't be changed later.</div>
                        </div>
                    {/if}
                    <div class="mb-3">
                        <label class="form-label" for="res-lab">Lab</label>
                        <select class="form-select" id="res-lab" bind:value={editingResource.lab}>
                            <option value="mech">Mech Lab</option>
                            <option value="mocap">Mocap Lab</option>
                        </select>
                    </div>
                    <div class="mb-3">
                        <label class="form-check">
                            <input class="form-check-input" type="checkbox" bind:checked={editingResource.requires_thread} />
                            <span class="form-check-label">Only research threads with access can book it</span>
                        </label>
                        <label class="form-check">
                            <input class="form-check-input" type="checkbox" bind:checked={editingResource.requires_approval} />
                            <span class="form-check-label">Every booking needs a lab admin's approval</span>
                        </label>
                        <label class="form-check mb-0">
                            <input class="form-check-input" type="checkbox" bind:checked={editingResource.active} />
                            <span class="form-check-label">Bookable</span>
                        </label>
                    </div>
                    <div class="row g-3">
                        <div class="col-12 col-sm-6">
                            <label class="form-label" for="res-hours">Longest booking, hours</label>
                            <input class="form-control" id="res-hours" type="number" min="0" max="168" bind:value={editingResource.max_hours} />
                            <div class="form-hint">0 means 12</div>
                        </div>
                        <div class="col-12 col-sm-6">
                            <label class="form-label" for="res-future">Upcoming bookings per person</label>
                            <input class="form-control" id="res-future" type="number" min="0" bind:value={editingResource.max_future_bookings} />
                            <div class="form-hint">0 means no limit</div>
                        </div>
                    </div>
                </div>
                <div class="modal-footer">
                    <button type="button" class="btn btn-outline-secondary" on:click={() => (editingResource = null)}>Cancel</button>
                    <button type="submit" class="btn btn-primary">Save</button>
                </div>
            </form>
        </div>
    </div>
    <div class="modal-backdrop fade show"></div>
{/if}

<!-- One person's exceptions to their group's permissions -->
{#if editingUser}
    <div class="modal modal-blur d-block" tabindex="-1" role="dialog" aria-modal="true" aria-labelledby="access-modal-title">
        <div class="modal-dialog modal-dialog-centered modal-dialog-scrollable modal-lg" role="document">
            <div class="modal-content">
                <div class="modal-header">
                    <h5 class="modal-title" id="access-modal-title">
                        Access for {editingUser.name}
                        <span class="text-secondary fw-normal">({groupName(editingUser.group_id)})</span>
                    </h5>
                    <button type="button" class="btn-close" aria-label="Close" on:click={() => (editingUser = null)}></button>
                </div>
                <div class="modal-body">
                    <p class="text-secondary">"Group default" follows the group's settings. Grant or revoke to make an exception for this person.</p>
                    <div class="row g-2">
                        {#each draftPerms as perm}
                            <div class="col-12 col-md-6">
                                <label class="d-flex align-items-center justify-content-between gap-2 border rounded p-2 h-100">
                                    <span>{permLabel(perm)}</span>
                                    <select
                                        class="form-select form-select-sm w-auto"
                                        class:border-success={overrideDraft[perm] === 'grant'}
                                        class:border-danger={overrideDraft[perm] === 'revoke'}
                                        bind:value={overrideDraft[perm]}
                                    >
                                        <option value={undefined}>Group default{groups.find((g) => g.id === editingUser.group_id)?.permissions.includes(perm) ? ' (yes)' : ' (no)'}</option>
                                        <option value="grant">Grant</option>
                                        <option value="revoke">Revoke</option>
                                    </select>
                                </label>
                            </div>
                        {/each}
                    </div>
                </div>
                <div class="modal-footer">
                    <button type="button" class="btn btn-outline-secondary" on:click={() => (editingUser = null)}>Cancel</button>
                    <button type="button" class="btn btn-primary" on:click={saveOverrides}>Save</button>
                </div>
            </div>
        </div>
    </div>
    <div class="modal-backdrop fade show"></div>
{/if}

<style>
    /* Only what Tabler has no class for */
    .admin-nav {
        position: sticky;
        top: 1rem;
    }

    .min-w-0 {
        min-width: 0;
    }

    /* Filter strips scroll sideways on a phone rather than wrapping */
    .scroll-x {
        overflow-x: auto;
        max-width: 100%;
        -webkit-overflow-scrolling: touch;
    }

    .scroll-x :global(.nav-segmented) {
        flex-wrap: nowrap;
        white-space: nowrap;
    }

    .thumb {
        width: 2rem;
        height: 2rem;
        flex-shrink: 0;
        object-fit: cover;
        border-radius: var(--tblr-border-radius);
        border: 1px solid var(--tblr-border-color);
    }

    .purpose-cell,
    .issue-cell {
        min-width: 12rem;
        max-width: 22rem;
    }

    .item-cell {
        min-width: 12rem;
    }

    .file-cell {
        max-width: 18rem;
    }

    .merge-group {
        width: 15rem;
    }

    .group-select {
        min-width: 8rem;
    }

    tr.past {
        opacity: 0.6;
    }

    .res-dot {
        width: 0.625rem;
        height: 0.625rem;
        border-radius: 50%;
        flex-shrink: 0;
    }

    /* Printers */
    .printer-img {
        width: 2.5rem;
        height: 2.5rem;
        object-fit: contain;
        flex-shrink: 0;
    }

    .camera {
        aspect-ratio: 16 / 9;
        overflow: hidden;
        border: 1px solid var(--tblr-border-color);
        border-radius: var(--tblr-border-radius-lg);
        background: var(--tblr-bg-surface-secondary);
    }

    .camera > img {
        display: block;
        width: 100%;
        height: 100%;
        object-fit: cover;
    }

    .camera-empty {
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        gap: 0.5rem;
        height: 100%;
    }

    .camera-empty img {
        width: 4rem;
        opacity: 0.5;
    }

    .ams-slots {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(4.25rem, 1fr));
        gap: 0.375rem;
    }

    .ams-slot {
        display: flex;
        flex-direction: column;
        gap: 0.25rem;
        min-width: 0;
        padding: 0.4rem;
        border: 1px solid var(--tblr-border-color);
        border-radius: var(--tblr-border-radius);
    }

    .ams-slot.active {
        border-color: var(--tblr-success);
    }

    .ams-slot.empty {
        opacity: 0.55;
    }

    .swatch {
        display: inline-block;
        width: 0.875rem;
        height: 0.875rem;
        flex-shrink: 0;
        border-radius: 50%;
        border: 1px solid var(--tblr-border-color);
    }

    .pick-swatch {
        width: 2.5rem;
        height: 2.5rem;
        border-radius: var(--tblr-border-radius);
        border: 2px solid var(--tblr-border-color);
        color: #000;
        font-weight: 700;
        font-size: 0.75rem;
        text-shadow: 0 0 3px rgba(255, 255, 255, 0.8);
        cursor: pointer;
    }

    .pick-swatch.picked {
        border-color: var(--tblr-primary);
        box-shadow: 0 0 0 2px var(--tblr-primary);
    }
</style>
