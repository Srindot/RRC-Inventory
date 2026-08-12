<script>
    import { onMount } from 'svelte';

    // Authentication state
    let isLoggedIn = false;
    let adminInfo = null;
    let authToken = '';
    let apiBase = ''; // set when logging in through the IP fallback

    // Login form
    let loginForm = {
        username: '',
        password: ''
    };
    // Fallback: allow entering server IP when mDNS/name resolution fails
    let showIpFallback = false;
    let altHost = '';
    let ipLoading = false;
    
    // Current view
    let currentView = 'login'; // login, dashboard, printers, lost-missing, history, lab-view, admin-management, change-password
    let selectedLab = '';
    let selectedFilter = 'all'; // all, borrowed, returned, not_found

    // Data
    let lostMissingItems = [];
    let bookings = [];

    // Printer control, mirrored from the public page so admins never have to
    // leave the dashboard to stop a job or fix an access code.
    // Booking calendar (same week view as the public page, with admin powers)
    const DAY_START = 8;
    const DAY_END = 22;
    const HOURS = Array.from({ length: DAY_END - DAY_START }, (_, i) => DAY_START + i);
    const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    let weekStart = startOfWeek(new Date());
    let selectedBooking = null;
    let showBookingForm = false;
    let bookingForm = {
        booked_by: '',
        phone: '',
        purpose: '',
        date: toDateInput(new Date()),
        start_hour: '10:00',
        end_hour: '12:00'
    };
    let savingBooking = false;

    let printers = [];
    let printerTimer;
    let cameraTimer;
    // Bumped on a timer so the <img> refetches; the P1S manages ~0.5 fps
    let cameraTick = Date.now();
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

    $: weekDays = Array.from({ length: 7 }, (_, i) => addDays(weekStart, i));
    $: calendarColumns = weekDays.map((day) => ({ day, blocks: layOutDay(day, bookings) }));
    $: weekLabel = `${weekDays[0].toLocaleDateString(undefined, { day: 'numeric', month: 'short' })} - ` +
                   `${weekDays[6].toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })}`;

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

    // Admin Management
    let adminList = [];
    let showCreateAdminForm = false;
    let createAdminForm = {
        username: '',
        password: '',
        name: '',
        is_super_admin: false
    };
    let changePasswordForm = {
        old_password: '',
        new_password: '',
        confirm_password: ''
    };

    // Lab options
    const labs = ['Main Lab', 'Mech Lab', 'Control Lab'];

    onMount(async () => {
        // Restore a previous session, if the token is still valid
        const savedAdmin = localStorage.getItem('adminInfo');
        const savedToken = localStorage.getItem('adminToken');
        apiBase = localStorage.getItem('adminApiBase') || '';
        if (savedAdmin && savedToken) {
            adminInfo = JSON.parse(savedAdmin);
            authToken = savedToken;
            isLoggedIn = true;
            currentView = 'dashboard';
            const response = await apiFetch('/api/admin/me');
            if (response && response.ok) {
                loadLostMissingItems();
            }
        }
    });

    // Authenticated fetch. Clears the session and returns to login on 401.
    async function apiFetch(path, options = {}) {
        const headers = { ...(options.headers || {}) };
        if (authToken) {
            headers['Authorization'] = `Bearer ${authToken}`;
        }

        let response;
        try {
            response = await fetch(`${apiBase}${path}`, { ...options, headers });
        } catch (e) {
            showMessage('Network error. Please try again.', 'error');
            return null;
        }

        if (response.status === 401) {
            clearSession();
            showMessage('Your session expired. Please log in again.', 'error');
            return null;
        }
        return response;
    }

    function saveSession(data, base = '') {
        adminInfo = data.admin;
        authToken = data.token;
        apiBase = base;
        localStorage.setItem('adminInfo', JSON.stringify(adminInfo));
        localStorage.setItem('adminToken', authToken);
        localStorage.setItem('adminApiBase', base);
        isLoggedIn = true;
        currentView = 'dashboard';
        loginForm = { username: '', password: '' };
        loadLostMissingItems();
    }

    function clearSession() {
        clearInterval(printerTimer);
        clearInterval(cameraTimer);
        localStorage.removeItem('adminInfo');
        localStorage.removeItem('adminToken');
        localStorage.removeItem('adminApiBase');
        adminInfo = null;
        authToken = '';
        apiBase = '';
        isLoggedIn = false;
        currentView = 'login';
    }

    // Login function
    async function login() {
        loading = true;
        try {
            const response = await fetch('/api/admin/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(loginForm)
            });

            if (response.ok) {
                saveSession(await response.json());
                showMessage('Login successful!', 'success');
            } else {
                const error = await response.json();
                showMessage(error.error || 'Login failed', 'error');
            }
        } catch (e) {
            // Network error (could be mDNS/name resolution). Offer IP fallback.
            showMessage('Login failed (network). If name resolution fails, try the server IP below.', 'error');
            showIpFallback = true;
        } finally {
            loading = false;
        }
    }

    // Try login by supplying an explicit host/IP (e.g. 10.2.36.243)
    async function tryIpLogin() {
        if (!altHost) {
            showMessage('Please enter the server IP (e.g. 10.2.36.243)', 'error');
            return;
        }
        ipLoading = true;
        try {
            // Trim possible http(s) prefix
            const hostOnly = altHost.replace(/^https?:\/\//, '').replace(/\/.*/, '');
            const base = `http://${hostOnly}`;
            const response = await fetch(`${base}/api/admin/login`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(loginForm)
            });

            if (response.ok) {
                saveSession(await response.json(), base);
                showMessage('Login successful (via IP)!', 'success');
            } else {
                const error = await response.json().catch(() => ({}));
                showMessage(error.error || `Login failed (HTTP ${response.status})`, 'error');
            }
        } catch (err) {
            showMessage('Failed to contact server at that IP. Check network or try another IP.', 'error');
        } finally {
            ipLoading = false;
        }
    }

    // Logout function
    async function logout() {
        await apiFetch('/api/admin/logout', { method: 'POST' });
        clearSession();
        showMessage('Logged out successfully', 'success');
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
                    extend_hours: parseInt(hours) || 0,
                    admin_name: adminInfo.name
                })
            });

            if (response && response.ok) {
                showMessage('Loan extended successfully!', 'success');
                if (currentView === 'lab-view') {
                    loadLabLoans(selectedLab, selectedFilter);
                }
            } else if (response) {
                const error = await response.json();
                showMessage(error.error || 'Failed to extend loan', 'error');
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
                const error = await response.json();
                showMessage(error.error || 'Failed to mark item as missing', 'error');
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
                const error = await response.json();
                showMessage(error.error || 'Failed to mark item as found', 'error');
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

    // Load Motion Capture Lab bookings (from a week ago onwards)
    async function loadBookings() {
        loading = true;
        try {
            const from = new Date();
            from.setDate(from.getDate() - 7);
            const response = await apiFetch(`/api/bookings?from=${from.toISOString()}`);
            if (response && response.ok) {
                bookings = await response.json();
            } else if (response) {
                showMessage('Failed to load bookings', 'error');
            }
        } finally {
            loading = false;
        }
    }

    async function deleteBooking(bookingId, bookedBy) {
        if (!confirm(`Delete the Motion Capture Lab booking by ${bookedBy}?`)) {
            return;
        }

        const response = await apiFetch(`/api/admin/bookings/${bookingId}`, { method: 'DELETE' });
        if (!response) return;

        if (response.ok) {
            showMessage('Booking deleted', 'success');
            loadBookings();
        } else {
            const error = await response.json();
            showMessage(error.error || 'Failed to delete booking', 'error');
        }
    }

    // --- booking calendar --------------------------------------------------

    function startOfWeek(date) {
        const d = new Date(date);
        d.setHours(0, 0, 0, 0);
        d.setDate(d.getDate() - ((d.getDay() + 6) % 7)); // Monday first
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

    function bookingTime(value) {
        return new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }

    // One column per day with its bookings already positioned
    function layOutDay(day, allBookings) {
        return allBookings
            .map((b) => ({ ...b, start: new Date(b.start_time), end: new Date(b.end_time) }))
            .filter((b) => sameDay(b.start, day))
            .map((b) => {
                const from = Math.max(b.start.getHours() + b.start.getMinutes() / 60, DAY_START);
                const to = Math.min(b.end.getHours() + b.end.getMinutes() / 60, DAY_END);
                return {
                    ...b,
                    top: ((from - DAY_START) / (DAY_END - DAY_START)) * 100,
                    height: (Math.max(to - from, 0.5) / (DAY_END - DAY_START)) * 100
                };
            });
    }

    function goToWeek(offset) {
        weekStart = addDays(weekStart, offset * 7);
    }

    function openSlot(day, hour) {
        bookingForm = {
            ...bookingForm,
            date: toDateInput(day),
            start_hour: `${String(hour).padStart(2, '0')}:00`,
            end_hour: `${String(Math.min(hour + 2, DAY_END)).padStart(2, '0')}:00`
        };
        showBookingForm = true;
        selectedBooking = null;
    }

    // Admins can book on someone's behalf - people ask in person constantly
    async function createBooking() {
        const start = new Date(`${bookingForm.date}T${bookingForm.start_hour}`);
        const end = new Date(`${bookingForm.date}T${bookingForm.end_hour}`);

        if (!(end > start)) {
            showMessage('End time must be after the start time', 'error');
            return;
        }

        savingBooking = true;
        try {
            const response = await apiFetch('/api/bookings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    booked_by: bookingForm.booked_by,
                    phone: bookingForm.phone,
                    purpose: bookingForm.purpose,
                    start_time: start.toISOString(),
                    end_time: end.toISOString()
                })
            });
            if (!response) return;

            const result = await response.json();
            if (response.ok) {
                showMessage('Booking added', 'success');
                showBookingForm = false;
                bookingForm = { ...bookingForm, purpose: '' };
                weekStart = startOfWeek(start);
                loadBookings();
            } else {
                showMessage(result.error || 'Could not add the booking', 'error');
            }
        } finally {
            savingBooking = false;
        }
    }

    // Admins delete without needing the booker's phone number
    async function deleteSelectedBooking() {
        if (!selectedBooking) return;
        const booking = selectedBooking;
        if (!confirm(`Delete ${booking.booked_by}'s booking?\n\n${booking.purpose}`)) {
            return;
        }

        const response = await apiFetch(`/api/admin/bookings/${booking.ID}`, { method: 'DELETE' });
        if (!response) return;

        if (response.ok) {
            showMessage('Booking deleted', 'success');
            selectedBooking = null;
            loadBookings();
        } else {
            const error = await response.json();
            showMessage(error.error || 'Could not delete the booking', 'error');
        }
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
        clearInterval(printerTimer);
        clearInterval(cameraTimer);
        printerTimer = setInterval(loadPrinters, 5000);
        cameraTimer = setInterval(() => (cameraTick = Date.now()), 2000);
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

            const result = await response.json();
            if (response.ok) {
                showMessage(`Stop command sent to ${printer.name}`, 'success');
                loadPrinters();
            } else {
                showMessage(result.error || 'Could not stop the print', 'error');
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

            const result = await response.json();
            if (response.ok) {
                showMessage(`${printer.name}: access code updated, reconnecting...`, 'success');
                editingCode = '';
                newCode = '';
                setTimeout(loadPrinters, 4000);
                setTimeout(loadPrinters, 12000);
            } else {
                showMessage(result.error || 'Could not update the access code', 'error');
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
    function showMessage(text, type) {
        message = text;
        messageType = type;
        setTimeout(() => {
            message = '';
            messageType = '';
        }, 5000);
    }

    function formatDate(dateString) {
        return new Date(dateString).toLocaleDateString();
    }

    function formatExpectedReturn(dateString) {
        const expectedDate = new Date(dateString);
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

    // An item is due at the END of its return date, so something due today is
    // not late yet. Matches the backend and the admin sort order.
    function dueDeadline(returnDate) {
        const due = new Date(returnDate);
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
    }

    function showItemHistory() {
        currentView = 'history';
        loadHistoryItems();
    }

    function showLabView(lab) {
        currentView = 'lab-view';
        selectedFilter = 'all'; // Reset filter when switching labs
        loadLabLoans(lab, 'all');
    }

    function filterLabLoans(filter) {
        selectedFilter = filter;
        loadLabLoans(selectedLab, filter);
    }

    function goToDashboard() {
        currentView = 'dashboard';
        clearInterval(printerTimer);
        clearInterval(cameraTimer);
    }

    // Admin Management Functions
    function showAdminManagement() {
        currentView = 'admin-management';
        loadAdminList();
    }

    function showChangePasswordView() {
        currentView = 'change-password';
    }

    async function loadAdminList() {
        if (!adminInfo?.is_super_admin) return;
        
        const response = await apiFetch('/api/admin/list');
        if (response && response.ok) {
            adminList = await response.json();
        } else if (response) {
            showMessage('Failed to load admin list', 'error');
        }
    }

    async function createAdmin() {
        if (!adminInfo?.is_super_admin) return;
        
        const response = await apiFetch('/api/admin/create', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(createAdminForm)
        });
        if (!response) return;

        const result = await response.json();
        if (response.ok) {
            showMessage('Admin created successfully', 'success');
            showCreateAdminForm = false;
            createAdminForm = { username: '', password: '', name: '', is_super_admin: false };
            loadAdminList();
        } else {
            showMessage(result.error || 'Failed to create admin', 'error');
        }
    }

    async function changePassword() {
        if (changePasswordForm.new_password !== changePasswordForm.confirm_password) {
            showMessage('New passwords do not match', 'error');
            return;
        }

        if (changePasswordForm.new_password.length < 8) {
            showMessage('New password must be at least 8 characters long', 'error');
            return;
        }

        const response = await apiFetch('/api/admin/change-password', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                old_password: changePasswordForm.old_password,
                new_password: changePasswordForm.new_password
            })
        });
        if (!response) return;

        const result = await response.json();
        if (response.ok) {
            showMessage('Password changed successfully', 'success');
            changePasswordForm = { old_password: '', new_password: '', confirm_password: '' };
            currentView = 'dashboard';
        } else {
            showMessage(result.error || 'Failed to change password', 'error');
        }
    }

    async function deleteAllItems() {
        if (!adminInfo?.is_super_admin) return;
        
        if (!confirm('⚠️ WARNING: This will permanently delete ALL items data!\n\nThis action cannot be undone. Are you sure you want to continue?')) {
            return;
        }

        const response = await apiFetch('/api/admin/delete-all-items', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ confirm_delete: true })
        });
        if (!response) return;

        const result = await response.json();
        if (response.ok) {
            showMessage(`Successfully deleted ${result.deleted_count} items`, 'success');
        } else {
            showMessage(result.error || 'Failed to delete items', 'error');
        }
    }

    async function deleteAdmin(adminId, adminName, adminUsername) {
        if (!adminInfo?.is_super_admin) return;

        // Prevent deleting self
        if (adminUsername === adminInfo.username) {
            showMessage('Cannot delete yourself', 'error');
            return;
        }


        if (!confirm(`⚠️ WARNING: This will permanently delete the admin account!\n\nAdmin: ${adminName} (@${adminUsername})\n\nThis action cannot be undone. Are you sure you want to continue?`)) {
            return;
        }

        const response = await apiFetch(`/api/admin/delete/${adminId}`, {
            method: 'DELETE'
        });
        if (!response) return;

        const result = await response.json();
        if (response.ok) {
            showMessage(`Successfully deleted admin: ${result.deleted_admin.name}`, 'success');
            loadAdminList(); // Reload the admin list
        } else {
            showMessage(result.error || 'Failed to delete admin', 'error');
        }
    }

</script>

<div class="admin-container">
    <!-- Message Display -->
    {#if message}
        <div class="message {messageType}">
            {message}
        </div>
    {/if}

    <!-- Login View -->
    {#if !isLoggedIn}
        <div class="login-container">
            <div class="login-header">
                <button class="close-btn" on:click={() => window.location.href = '/'} title="Go back to main page">
                    ✕
                </button>
                <img src="/rrc_logo.png" alt="RRC Logo" class="login-logo" />
                <h1>Admin Login</h1>
            </div>
            <div class="login-form">
                <form on:submit|preventDefault={login}>
                    <div class="form-group">
                        <label for="username">Username</label>
                        <input 
                            type="text" 
                            id="username" 
                            bind:value={loginForm.username} 
                            required
                            placeholder="Enter username"
                        />
                    </div>
                    <div class="form-group">
                        <label for="password">Password</label>
                        <input 
                            type="password" 
                            id="password" 
                            bind:value={loginForm.password} 
                            required
                            placeholder="Enter password"
                        />
                    </div>
                    <button type="submit" class="login-btn" disabled={loading}>
                        {loading ? 'Logging in...' : 'Login'}
                    </button>
                    {#if showIpFallback}
                        <div class="ip-fallback" style="margin-top:12px;">
                            <label for="altHost">Server IP (fallback):</label>
                            <div style="display:flex;gap:8px;margin-top:6px;align-items:center;">
                                <input id="altHost" type="text" bind:value={altHost} placeholder="10.2.36.243" />
                                <button type="button" class="login-btn" on:click={tryIpLogin} disabled={ipLoading}>{ipLoading ? 'Trying...' : 'Login via IP'}</button>
                            </div>
                        </div>
                    {/if}
                </form>
            </div>
        </div>
    {/if}

    <!-- Admin Dashboard -->
    {#if isLoggedIn}
        <div class="dashboard">
            <!-- Header -->
            <div class="dashboard-header">
                <div class="dashboard-title">
                    <img src="/rrc_logo.png" alt="RRC Logo" class="admin-logo" />
                    <h1>Robotics Research Centre - Admin Dashboard</h1>
                </div>
                <div class="admin-info">
                    <span>Welcome, {adminInfo.name}</span>
                    <button class="logout-btn" on:click={logout}>Logout</button>
                </div>
            </div>

            <!-- Navigation -->
            <div class="nav-tabs">
                <button 
                    class="tab-btn" 
                    class:active={currentView === 'dashboard'}
                    on:click={goToDashboard}
                >
                    Dashboard
                </button>
                <button 
                    class="tab-btn" 
                    class:active={currentView === 'lost-missing'}
                    on:click={showLostMissingItems}
                >
                    🔍 Missing Items
                    {#if lostMissingItems.length > 0}
                        <span class="badge">{lostMissingItems.length}</span>
                    {/if}
                </button>
                {#each labs as lab}
                    <button 
                        class="tab-btn" 
                        class:active={currentView === 'lab-view' && selectedLab === lab}
                        on:click={() => showLabView(lab)}
                    >
                        {lab}
                    </button>
                {/each}
                <button 
                    class="tab-btn" 
                    class:active={currentView === 'printers'}
                    on:click={showPrinters}
                >
                    🖨️ Printers
                </button>
                <button 
                    class="tab-btn" 
                    class:active={currentView === 'bookings'}
                    on:click={showBookings}
                >
                    🎥 Mocap Bookings
                </button>
                <button 
                    class="tab-btn" 
                    class:active={currentView === 'history'}
                    on:click={showItemHistory}
                >
                    📚 Item History
                </button>
                <button 
                    class="tab-btn" 
                    class:active={currentView === 'change-password'}
                    on:click={showChangePasswordView}
                >
                    🔑 Change Password
                </button>
                {#if adminInfo?.is_super_admin}
                    <button 
                        class="tab-btn" 
                        class:active={currentView === 'admin-management'}
                        on:click={showAdminManagement}
                    >
                        👥 Admin Management
                    </button>
                {/if}
            </div>

            <!-- Dashboard View -->
            {#if currentView === 'dashboard'}
                <div class="dashboard-content">
                    <h2>System Overview</h2>
                    <div class="overview-cards">
                        <div class="card">
                            <h3>🔍 Missing Items</h3>
                            <p class="stat-number">{lostMissingItems.length}</p>
                            <button class="card-btn" on:click={showLostMissingItems}>Review</button>
                        </div>
                        <div class="card">
                            <h3>🖨️ 3D Printers</h3>
                            <p>Live status, stop a print</p>
                            <button class="card-btn" on:click={showPrinters}>Manage</button>
                        </div>
                        <div class="card">
                            <h3>🎥 Mocap Lab</h3>
                            <p>Motion Capture Lab bookings</p>
                            <button class="card-btn" on:click={showBookings}>Manage</button>
                        </div>
                        {#each labs as lab}
                            <div class="card">
                                <h3>🏭 {lab}</h3>
                                <p>Manage borrowed items</p>
                                <button class="card-btn" on:click={() => showLabView(lab)}>View</button>
                            </div>
                        {/each}
                    </div>
                    <div class="admin-actions">
                        <button class="export-btn" on:click={() => exportCSV()}>
                            📊 Export Loans (CSV)
                        </button>
                        <button class="export-btn" on:click={() => exportCSV('/api/admin/export-bookings-csv', 'motion_capture_lab_bookings.csv')}>
                            🎥 Export Bookings (CSV)
                        </button>
                    </div>
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
                                                    <img 
                                                        src="/api/photos/{loan.photo_filename}" 
                                                        alt="{loan.item_name}"
                                                        on:error={(e) => e.target.style.display = 'none'}
                                                    />
                                                </div>
                                            {:else}
                                                <div class="item-image placeholder">
                                                    <span>📷</span>
                                                </div>
                                            {/if}
                                        </div>
                                        <div class="status-badges">
                                            <span class="status-badge not-found">Missing</span>
                                        </div>
                                    </div>
                                    <div class="loan-details">
                                        <p><strong>Borrower:</strong> {loan.borrower_name}</p>
                                        <p><strong>Phone:</strong> 
                                            <span 
                                                class="clickable-phone" 
                                                role="button"
                                                tabindex="0"
                                                on:click={() => copyToClipboard(loan.borrower_phone)}
                                                on:keydown={(e) => e.key === 'Enter' && copyToClipboard(loan.borrower_phone)}
                                                title="Click to copy phone number"
                                            >
                                                {loan.borrower_phone}
                                            </span>
                                        </p>
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
                                                <button 
                                                    class="found-btn" 
                                                    on:click={() => markAsFound(loan.ID)}
                                                    disabled={loading}
                                                >
                                                    ✅ Mark as Found
                                                </button>
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
                        Live status, refreshing every few seconds.
                        <a class="calendar-link" href="/printers" target="_blank" rel="noopener">Open the full page →</a>
                    </p>

                    {#if printers.length === 0}
                        <p class="no-items">No printers are configured.</p>
                    {:else}
                        <div class="printer-admin-grid">
                            {#each printers as printer, i (printer.id)}
                                <div class="printer-admin-card {printerStateClass(printer)}" style="--i: {i}">
                                    <div class="pa-head">
                                        <img src="/P1S.png" alt="" class="pa-image" />
                                        <div class="pa-title">
                                            <h3>{printer.name}</h3>
                                            <span class="pa-avail {printerIsPrinting(printer) ? 'busy' : 'free'}">
                                                {printer.online ? (printerIsPrinting(printer) ? 'In use' : 'Free') : 'Unknown'}
                                            </span>
                                        </div>
                                        <span class="pa-state {printerStateClass(printer)}">
                                            {printerStateLabel(printer)}
                                        </span>
                                    </div>

                                    <div class="pa-camera">
                                        {#if printer.camera_online}
                                            <img
                                                src="/api/printers/{printer.id}/snapshot?t={cameraTick}"
                                                alt="Camera view of {printer.name}"
                                            />
                                        {:else}
                                            <div class="pa-camera-empty">
                                                <img src="/P1S.png" alt="" />
                                                <p>No camera image</p>
                                            </div>
                                        {/if}
                                    </div>

                                    {#if printer.access_code_problem}
                                        <div class="pa-warning">
                                            <strong>⚠️ Access code changed</strong>
                                            <p>{printer.name} is refusing our access code - LAN mode was probably toggled.</p>
                                            {#if editingCode === printer.id}
                                                <div class="pa-code-form">
                                                    <input
                                                        type="text"
                                                        bind:value={newCode}
                                                        placeholder="New access code"
                                                        autocomplete="off"
                                                    />
                                                    <button class="pa-save" on:click={() => savePrinterCode(printer)} disabled={savingCode}>
                                                        {savingCode ? 'Saving...' : 'Save'}
                                                    </button>
                                                    <button class="pa-cancel" on:click={() => (editingCode = '')}>Cancel</button>
                                                </div>
                                            {:else}
                                                <button class="pa-fix" on:click={() => { editingCode = printer.id; newCode = ''; }}>
                                                    Update access code
                                                </button>
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

                                        <div class="pa-temps">
                                            <span>🔥 {printer.nozzle_temp.toFixed(0)}°C</span>
                                            <span>🛏️ {printer.bed_temp.toFixed(0)}°C</span>
                                            {#if printer.chamber_temp > 0}
                                                <span>📦 {printer.chamber_temp.toFixed(0)}°C</span>
                                            {/if}
                                        </div>

                                        {#if printerIsPrinting(printer)}
                                            <button
                                                class="pa-stop"
                                                on:click={() => stopPrint(printer)}
                                                disabled={stoppingPrinter === printer.id}
                                            >
                                                {stoppingPrinter === printer.id ? 'Stopping...' : '⏹ Stop Print'}
                                            </button>
                                        {/if}

                                        {#if printer.last_action_by}
                                            <p class="pa-last">Last stopped by {printer.last_action_by}</p>
                                        {/if}
                                    {:else}
                                        <p class="pa-offline">Not responding - switched off or off the network.</p>
                                    {/if}
                                </div>
                            {/each}
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Motion Capture Lab Bookings View -->
            {#if currentView === 'bookings'}
                <div class="loans-content">
                    <h2>🎥 Motion Capture Lab Bookings</h2>
                    <p class="subtitle-text">
                        Bookings from the last week onwards.
                        <a class="calendar-link" href="/mocap" target="_blank" rel="noopener">Open calendar view →</a>
                    </p>
                    <div class="cal-toolbar">
                        <div class="cal-nav">
                            <button on:click={() => goToWeek(-1)} aria-label="Previous week">◀</button>
                            <button class="cal-today" on:click={() => (weekStart = startOfWeek(new Date()))}>Today</button>
                            <button on:click={() => goToWeek(1)} aria-label="Next week">▶</button>
                            <span class="cal-label">{weekLabel}</span>
                        </div>
                        <button class="cal-add" on:click={() => { showBookingForm = !showBookingForm; selectedBooking = null; }}>
                            {showBookingForm ? '✕ Close' : '➕ Add Booking'}
                        </button>
                    </div>

                    {#if showBookingForm}
                        <form class="cal-form" on:submit|preventDefault={createBooking}>
                            <p class="cal-form-hint">Booking on someone's behalf - click a slot in the calendar to prefill the time.</p>
                            <div class="cal-form-row">
                                <label>
                                    Name
                                    <input type="text" bind:value={bookingForm.booked_by} required placeholder="Who is it for" />
                                </label>
                                <label>
                                    Phone
                                    <input type="tel" bind:value={bookingForm.phone} required placeholder="Their number" />
                                </label>
                            </div>
                            <div class="cal-form-row">
                                <label>
                                    Date
                                    <input type="date" bind:value={bookingForm.date} required />
                                </label>
                                <label>
                                    From
                                    <input type="time" bind:value={bookingForm.start_hour} required step="900" />
                                </label>
                                <label>
                                    To
                                    <input type="time" bind:value={bookingForm.end_hour} required step="900" />
                                </label>
                            </div>
                            <label class="cal-form-full">
                                Purpose
                                <input type="text" bind:value={bookingForm.purpose} required placeholder="What for" />
                            </label>
                            <button type="submit" class="cal-save" disabled={savingBooking}>
                                {savingBooking ? 'Adding...' : 'Add Booking'}
                            </button>
                        </form>
                    {/if}

                    {#if selectedBooking}
                        <div class="cal-details">
                            <h3>{selectedBooking.purpose}</h3>
                            <p><strong>Booked by:</strong> {selectedBooking.booked_by}</p>
                            <p><strong>When:</strong>
                                {new Date(selectedBooking.start_time).toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'short' })},
                                {bookingTime(selectedBooking.start_time)} - {bookingTime(selectedBooking.end_time)}</p>
                            <p><strong>Contact:</strong>
                                <span
                                    class="clickable-phone"
                                    role="button"
                                    tabindex="0"
                                    on:click={() => copyToClipboard(selectedBooking.phone)}
                                    on:keydown={(e) => e.key === 'Enter' && copyToClipboard(selectedBooking.phone)}
                                >{selectedBooking.phone}</span>
                            </p>
                            <div class="cal-details-actions">
                                <button class="cal-delete" on:click={deleteSelectedBooking}>🗑️ Delete Booking</button>
                                <button class="cal-close" on:click={() => (selectedBooking = null)}>Close</button>
                            </div>
                        </div>
                    {/if}

                    <div class="cal">
                        <div class="cal-head">
                            <div class="cal-gutter"></div>
                            {#each weekDays as day}
                                <div class="cal-day-head" class:today={isToday(day)}>
                                    <span class="cal-day-name">{DAY_NAMES[day.getDay()]}</span>
                                    <span class="cal-day-num">{day.getDate()}</span>
                                </div>
                            {/each}
                        </div>
                        <div class="cal-body">
                            <div class="cal-gutter">
                                {#each HOURS as hour}
                                    <div class="cal-time">{String(hour).padStart(2, '0')}:00</div>
                                {/each}
                            </div>
                            {#each calendarColumns as column}
                                <div class="cal-col" class:today={isToday(column.day)}>
                                    {#each HOURS as hour}
                                        <button
                                            class="cal-cell"
                                            on:click={() => openSlot(column.day, hour)}
                                            aria-label="Add a booking at {String(hour).padStart(2, '0')}:00 on {column.day.toDateString()}"
                                        ></button>
                                    {/each}
                                    {#each column.blocks as block (block.ID)}
                                        <button
                                            class="cal-block"
                                            style="top: {block.top}%; height: {block.height}%;"
                                            on:click={() => { selectedBooking = block; showBookingForm = false; }}
                                        >
                                            <span class="cal-block-time">{bookingTime(block.start)} - {bookingTime(block.end)}</span>
                                            <span class="cal-block-name">{block.booked_by}</span>
                                            <span class="cal-block-purpose">{block.purpose}</span>
                                        </button>
                                    {/each}
                                </div>
                            {/each}
                        </div>
                    </div>

                    <h3 class="cal-list-title">All bookings</h3>

                    {#if loading}
                        <p>Loading bookings...</p>
                    {:else if bookings.length === 0}
                        <p class="no-items">No bookings.</p>
                    {:else}
                        <div class="bookings-list">
                            {#each bookings as booking}
                                <div class="booking-row" class:past={new Date(booking.end_time) < new Date()}>
                                    <div class="booking-when">
                                        <span class="booking-date">
                                            {new Date(booking.start_time).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}
                                        </span>
                                        <span class="booking-time">
                                            {new Date(booking.start_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                            -
                                            {new Date(booking.end_time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                                        </span>
                                    </div>
                                    <div class="booking-info">
                                        <p class="booking-purpose">{booking.purpose}</p>
                                        <p class="booking-by">
                                            {booking.booked_by} ·
                                            <span 
                                                class="clickable-phone" 
                                                role="button"
                                                tabindex="0"
                                                on:click={() => copyToClipboard(booking.phone)}
                                                on:keydown={(e) => e.key === 'Enter' && copyToClipboard(booking.phone)}
                                                title="Click to copy phone number"
                                            >{booking.phone}</span>
                                        </p>
                                    </div>
                                    <button 
                                        class="delete-admin-btn" 
                                        on:click={() => deleteBooking(booking.ID, booking.booked_by)}
                                    >
                                        🗑️ Delete
                                    </button>
                                </div>
                            {/each}
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Item History View -->
            {#if currentView === 'history'}
                <div class="loans-content">
                    <h2>📚 Complete Item History</h2>
                    <p class="subtitle-text">Chronological history of all items - borrowed, returned and missing</p>
                    
                    <!-- Search functionality for history -->
                    <div class="history-search-container">
                        <label for="history-search">Search history by name, item, phone, lab, purpose, status, or admin:</label>
                        <div class="search-input-group">
                            <input 
                                type="text" 
                                id="history-search" 
                                bind:value={historySearchQuery}
                                placeholder="Search through all history records..."
                                autocomplete="off"
                                class="history-search-input"
                            />
                            {#if historySearchQuery && historySearchQuery.trim()}
                                <button 
                                    class="clear-search-btn" 
                                    on:click={clearHistorySearch} 
                                    title="Clear search"
                                    type="button"
                                >
                                    ✕
                                </button>
                            {/if}
                        </div>
                        {#if historySearchQuery.trim()}
                            <p class="search-results-info">
                                Showing {filteredHistoryItems.length} of {historyItems.length} history records
                            </p>
                        {/if}
                    </div>

                    {#if loading}
                        <p>Loading item history...</p>
                    {:else if historyItems.length === 0}
                        <p class="no-items">No history found.</p>
                    {:else if filteredHistoryItems.length === 0 && historySearchQuery.trim()}
                        <div class="no-search-results">
                            <p>No history records match your search for "{historySearchQuery}"</p>
                            <button class="clear-search-btn-large" on:click={clearHistorySearch}>
                                Clear Search
                            </button>
                        </div>
                    {:else}
                        <div class="history-list">
                            {#each filteredHistoryItems as item}
                                <div class="history-item {item.status}" data-status="{item.status}">
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
                                                        <img 
                                                            src="/api/photos/{item.photo_filename}" 
                                                            alt="{item.item_name}"
                                                            on:error={(e) => e.target.style.display = 'none'}
                                                        />
                                                    </div>
                                                {:else}
                                                    <div class="item-image placeholder">
                                                        <span>📷</span>
                                                    </div>
                                                {/if}
                                            </div>
                                            <div class="history-status-badges">
                                                <span class="status-badge {item.status}">
                                                    {#if item.status === 'returned'}
                                                        ✅ Returned
                                                    {:else if item.status === 'not_found'}
                                                        ❌ Missing
                                                    {:else if item.status === 'active'}
                                                        📋 Borrowed
                                                    {:else}
                                                        {item.status.charAt(0).toUpperCase() + item.status.slice(1)}
                                                    {/if}
                                                </span>
                                                {#if item.status === 'active' && isOverdue(item.expected_return_date)}
                                                    <span class="overdue-badge">OVERDUE</span>
                                                {/if}
                                            </div>
                                            <div class="history-date">
                                                <span class="date-label">
                                                    {#if item.status === 'returned'}
                                                        Returned:
                                                    {:else if item.status === 'not_found'}
                                                        Marked Missing:
                                                    {:else}
                                                        Borrowed:
                                                    {/if}
                                                </span>
                                                <span class="date-value">
                                                    {#if item.status === 'returned' && item.returned_at}
                                                        {formatDate(item.returned_at)}
                                                    {:else if item.status === 'not_found' && item.approved_at}
                                                        {formatDate(item.approved_at)}
                                                    {:else}
                                                        {formatDate(item.CreatedAt)}
                                                    {/if}
                                                </span>
                                            </div>
                                        </div>
                                        <div class="history-details">
                                            <div class="detail-row">
                                                <div class="detail-group">
                                                    <p><strong>Borrower:</strong> {item.borrower_name}</p>
                                                    <p><strong>Phone:</strong> 
                                                        <span 
                                                            class="clickable-phone" 
                                                            role="button"
                                                            tabindex="0"
                                                            on:click={() => copyToClipboard(item.borrower_phone)}
                                                            on:keydown={(e) => e.key === 'Enter' && copyToClipboard(item.borrower_phone)}
                                                            title="Click to copy phone number"
                                                        >
                                                            {item.borrower_phone}
                                                        </span>
                                                    </p>
                                                    <p><strong>Lab:</strong> {item.lab_location}</p>
                                                </div>
                                                <div class="detail-group">
                                                    <p><strong>Quantity:</strong> {item.quantity_borrowed}</p>
                                                    <p><strong>Purpose:</strong> {item.purpose}</p>
                                                    {#if item.approved_by}
                                                        <p><strong>Last handled by:</strong> {item.approved_by}</p>
                                                    {/if}
                                                </div>
                                                <div class="detail-group">
                                                    <p><strong>Requested:</strong> {formatDate(item.CreatedAt)}</p>
                                                    {#if item.expected_return_date}
                                                        <p><strong>Expected Return:</strong> {formatExpectedReturn(item.expected_return_date)}</p>
                                                    {/if}
                                                    {#if item.status === 'returned' && item.returned_at}
                                                        <p><strong>Return Date:</strong> {formatDate(item.returned_at)}</p>
                                                    {/if}
                                                    {#if item.status === 'returned' && isOverdue(item.expected_return_date)}
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
                    <p class="subtitle-text">Items returned more than 2 weeks ago are automatically archived. View complete history in the "� Item History" tab.</p>
                    
                    <!-- Status Filters -->
                    <div class="status-filters">
                        <button 
                            class="filter-btn" 
                            class:active={selectedFilter === 'all'}
                            on:click={() => filterLabLoans('all')}
                        >
                            All
                        </button>
                        <button 
                            class="filter-btn" 
                            class:active={selectedFilter === 'borrowed'}
                            on:click={() => filterLabLoans('borrowed')}
                        >
                            Borrowed
                        </button>
                        <button 
                            class="filter-btn" 
                            class:active={selectedFilter === 'returned'}
                            on:click={() => filterLabLoans('returned')}
                        >
                            Returned
                        </button>
                        <button 
                            class="filter-btn" 
                            class:active={selectedFilter === 'not_found'}
                            on:click={() => filterLabLoans('not_found')}
                        >
                            Missing
                        </button>
                    </div>

                    {#if loading}
                        <p>Loading lab items...</p>
                    {:else if labLoans.length === 0}
                        <p class="no-items">No items found for the selected filter.</p>
                    {:else}
                        <div class="loans-grid">
                            {#each labLoans as loan}
                                <div 
                                    class="loan-card" 
                                    class:overdue={isOverdue(loan.expected_return_date) && loan.status === 'active'}
                                >
                                    <div class="loan-header">
                                        <div class="loan-title-section">
                                            <h3>{loan.item_name}</h3>
                                            {#if loan.photo_filename}
                                                <div class="item-image">
                                                    <img 
                                                        src="/api/photos/{loan.photo_filename}" 
                                                        alt="{loan.item_name}"
                                                        on:error={(e) => e.target.style.display = 'none'}
                                                    />
                                                </div>
                                            {:else}
                                                <div class="item-image placeholder">
                                                    <span>📷</span>
                                                </div>
                                            {/if}
                                        </div>
                                        <div class="status-badges">
                                            {#if loan.status === 'not_found'}
                                                <span class="status-badge not-found">Missing</span>
                                            {:else if loan.status === 'returned'}
                                                <span class="status-badge returned">Returned</span>
                                            {:else}
                                                <span class="status-badge approved">Borrowed</span>
                                            {/if}
                                            {#if isOverdue(loan.expected_return_date) && loan.status === 'active'}
                                                <span class="overdue-badge">OVERDUE</span>
                                            {/if}
                                        </div>
                                    </div>
                                    <div class="loan-details">
                                        <p><strong>Borrower:</strong> {loan.borrower_name}</p>
                                        <p><strong>Phone:</strong> 
                                            <span 
                                                class="clickable-phone" 
                                                role="button"
                                                tabindex="0"
                                                on:click={() => copyToClipboard(loan.borrower_phone)}
                                                on:keydown={(e) => e.key === 'Enter' && copyToClipboard(loan.borrower_phone)}
                                                title="Click to copy phone number"
                                            >
                                                {loan.borrower_phone}
                                            </span>
                                        </p>
                                        <p><strong>Quantity:</strong> {loan.quantity_borrowed}</p>
                                        <p><strong>Purpose:</strong> {loan.purpose}</p>
                                        <p><strong>Expected Return:</strong> {formatExpectedReturn(loan.expected_return_date)}</p>
                                        <p><strong>Borrowed:</strong> {formatDate(loan.CreatedAt)}</p>
                                        {#if loan.approved_by}
                                            <p><strong>Last handled by:</strong> {loan.approved_by}</p>
                                        {/if}
                                    </div>
                                    {#if loan.status === 'active'}
                                        <div class="loan-actions">
                                            <div class="extend-controls">
                                                <label>Extend by:</label>
                                                <div class="extend-inputs">
                                                    <input 
                                                        type="number" 
                                                        id="extend-days-{loan.ID}"
                                                        min="0"
                                                        max="365"
                                                        placeholder="Days"
                                                        value="0"
                                                    />
                                                    <span>days</span>
                                                    <input 
                                                        type="number" 
                                                        id="extend-hours-{loan.ID}"
                                                        min="0"
                                                        max="23"
                                                        placeholder="Hours"
                                                        value="0"
                                                    />
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
                                            >
                                                📅 Extend
                                            </button>
                                            <button
                                                class="not-found-btn"
                                                on:click={() => markAsMissing(loan.ID, loan.item_name)}
                                                disabled={loading}
                                            >
                                                ❓ Can't Find This Item
                                            </button>
                                        </div>
                                    {/if}
                                    {#if loan.status === 'returned'}
                                        <div class="return-section">
                                            <p class="return-status approved">✅ Returned{#if loan.returned_at} on {formatDate(loan.returned_at)}{/if}</p>
                                            {#if isOverdue(loan.expected_return_date)}
                                                <p class="overdue-returned"><strong>Status:</strong> <span class="overdue-mark">⚠️ Returned Overdue</span></p>
                                            {/if}
                                        </div>
                                    {:else if loan.status === 'not_found'}
                                        <div class="found-section">
                                            <p class="found-notice">🔍 Item marked as missing</p>
                                            <div class="found-actions">
                                                <button
                                                    class="found-btn"
                                                    on:click={() => markAsFound(loan.ID)}
                                                    disabled={loading}
                                                >
                                                    ✅ Mark as Found
                                                </button>
                                            </div>
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
                        <form on:submit|preventDefault={changePassword}>
                            <div class="form-group">
                                <label for="old_password">Current Password</label>
                                <input 
                                    type="password" 
                                    id="old_password" 
                                    bind:value={changePasswordForm.old_password} 
                                    required
                                    placeholder="Enter current password"
                                />
                            </div>
                            <div class="form-group">
                                <label for="new_password">New Password</label>
                                <input 
                                    type="password" 
                                    id="new_password" 
                                    bind:value={changePasswordForm.new_password} 
                                    required
                                    minlength="6"
                                    placeholder="Enter new password (min 6 characters)"
                                />
                            </div>
                            <div class="form-group">
                                <label for="confirm_password">Confirm New Password</label>
                                <input 
                                    type="password" 
                                    id="confirm_password" 
                                    bind:value={changePasswordForm.confirm_password} 
                                    required
                                    minlength="6"
                                    placeholder="Confirm new password"
                                />
                            </div>
                            <div class="form-actions">
                                <button type="submit" class="submit-btn" disabled={loading}>
                                    {loading ? 'Changing...' : 'Change Password'}
                                </button>
                                <button type="button" class="cancel-btn" on:click={goToDashboard}>
                                    Cancel
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            {/if}

            <!-- Admin Management View (Super Admin Only) -->
            {#if currentView === 'admin-management' && adminInfo?.is_super_admin}
                <div class="admin-management-container">
                    <h2>👥 Admin Management</h2>
                    
                    <!-- Create Admin Section -->
                    <div class="management-section">
                        <div class="section-header">
                            <h3>Create New Admin</h3>
                            <button 
                                class="toggle-btn" 
                                on:click={() => showCreateAdminForm = !showCreateAdminForm}
                            >
                                {showCreateAdminForm ? '− Hide Form' : '+ Add Admin'}
                            </button>
                        </div>
                        
                        {#if showCreateAdminForm}
                            <div class="form-card">
                                <form on:submit|preventDefault={createAdmin}>
                                    <div class="form-group">
                                        <label for="create_username">Username</label>
                                        <input 
                                            type="text" 
                                            id="create_username" 
                                            bind:value={createAdminForm.username} 
                                            required
                                            placeholder="Enter username"
                                        />
                                    </div>
                                    <div class="form-group">
                                        <label for="create_password">Password</label>
                                        <input 
                                            type="password" 
                                            id="create_password" 
                                            bind:value={createAdminForm.password} 
                                            required
                                            minlength="6"
                                            placeholder="Enter password (min 6 characters)"
                                        />
                                    </div>
                                    <div class="form-group">
                                        <label for="create_name">Full Name</label>
                                        <input 
                                            type="text" 
                                            id="create_name" 
                                            bind:value={createAdminForm.name} 
                                            required
                                            placeholder="Enter full name"
                                        />
                                    </div>
                                    <div class="form-group">
                                        <label class="checkbox-label">
                                            <input 
                                                type="checkbox" 
                                                bind:checked={createAdminForm.is_super_admin}
                                            />
                                            Super Admin (can manage other admins)
                                        </label>
                                    </div>
                                    <div class="form-actions">
                                        <button type="submit" class="submit-btn" disabled={loading}>
                                            {loading ? 'Creating...' : 'Create Admin'}
                                        </button>
                                        <button type="button" class="cancel-btn" on:click={() => {
                                            showCreateAdminForm = false;
                                            createAdminForm = { username: '', password: '', name: '', is_super_admin: false };
                                        }}>
                                            Cancel
                                        </button>
                                    </div>
                                </form>
                            </div>
                        {/if}
                    </div>

                    <!-- Admin List Section -->
                    <div class="management-section">
                        <div class="section-header">
                            <h3>Current Admins</h3>
                            <button class="refresh-btn" on:click={loadAdminList}>🔄 Refresh</button>
                        </div>
                        
                        {#if adminList.length > 0}
                            <div class="admin-list">
                                {#each adminList as admin}
                                    <div class="admin-card">
                                        <div class="admin-info">
                                            <h4>{admin.name}</h4>
                                            <p class="admin-username">@{admin.username}</p>
                                            <p class="admin-role">
                                                {admin.is_super_admin ? '👑 Super Admin' : '👤 Admin'}
                                            </p>
                                            <p class="admin-date">
                                                Created: {new Date(admin.created_at).toLocaleDateString()}
                                            </p>
                                        </div>
                                        <div class="admin-actions">
                                            {#if admin.username === adminInfo.username}
                                                <span class="current-user-badge">You</span>
                                            {:else}
                                                <button 
                                                    class="delete-admin-btn" 
                                                    on:click={() => deleteAdmin(admin.id, admin.name, admin.username)}
                                                    title="Delete this admin account"
                                                >
                                                    🗑️ Delete
                                                </button>
                                            {/if}
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        {:else}
                            <p class="no-data">No admins found. <button class="link-btn" on:click={loadAdminList}>Refresh</button></p>
                        {/if}
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
                                <button class="danger-btn" on:click={deleteAllItems}>
                                    🗑️ Delete All Items
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            {/if}
        </div>
    {/if}
</div>

<!-- Credits Footer -->
<footer class="credits-footer">
    <p>Created by <strong>Srinath</strong> • <a href="https://github.com/Srindot" target="_blank">GitHub: Srindot</a></p>
    <p>Theme: <strong>Catppuccin Mocha</strong> • <a href="https://github.com/catppuccin/catppuccin" target="_blank">GitHub: Catppuccin</a></p>
</footer>

<style>
    :global(body) {
        background: var(--ctp-base);
        color: var(--ctp-text);
        font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
        margin: 0;
        padding: 0;
        min-height: 100vh;
    }

    .admin-container {
        max-width: 1400px;
        margin: 0 auto;
        padding: clamp(15px, 3vw, 30px);
        font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
        background: var(--ctp-mantle);
        height: 100vh;
        overflow-y: auto;
    }

    /* Message styles */
    .message {
        padding: clamp(12px, 3vw, 16px);
        border-radius: 8px;
        margin-bottom: 20px;
        text-align: center;
        font-weight: 500;
        animation: slideDown 0.3s ease;
    }

    .message.success {
        background-color: rgba(166, 227, 161, 0.15);
        border: 1px solid rgba(166, 227, 161, 0.4);
        color: var(--ctp-green);
    }

    .message.error {
        background-color: rgba(243, 139, 168, 0.15);
        border: 1px solid rgba(243, 139, 168, 0.4);
        color: var(--ctp-red);
    }

    @keyframes slideDown {
        from { opacity: 0; transform: translateY(-10px); }
        to { opacity: 1; transform: translateY(0); }
    }

    /* Login styles */
    .login-container {
        max-width: 450px;
        margin: clamp(50px, 10vh, 150px) auto;
        text-align: center;
    }

    .login-header {
        display: flex;
        align-items: center;
        justify-content: center;
        gap: clamp(15px, 3vw, 20px);
        margin-bottom: 30px;
        position: relative;
    }

    .close-btn {
        position: absolute;
        top: -10px;
        right: -10px;
        background: var(--ctp-red);
        color: var(--ctp-base);
        border: none;
        border-radius: 50%;
        width: 32px;
        height: 32px;
        font-size: 16px;
        font-weight: bold;
        cursor: pointer;
        display: flex;
        align-items: center;
        justify-content: center;
        transition: all 0.2s ease;
        box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
    }

    .close-btn:hover {
        background: var(--ctp-maroon);
        transform: scale(1.1);
    }

    .login-logo {
        height: clamp(50px, 8vw, 70px);
        width: auto;
        border-radius: 8px;
        box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
    }

    .login-header h1 {
        margin: 0;
        color: var(--ctp-text);
        font-size: clamp(1.6rem, 4vw, 2.2rem);
        font-weight: 600;
    }

    .login-form {
        background: var(--ctp-crust);
        padding: clamp(30px, 6vw, 50px);
        border-radius: 12px;
        box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
        border: 1px solid var(--ctp-surface0);
    }

    .form-group {
        margin-bottom: 20px;
        text-align: left;
    }

    .form-group label {
        display: block;
        margin-bottom: 8px;
        font-weight: 600;
        color: var(--ctp-text);
        font-size: clamp(0.9rem, 2vw, 1rem);
    }

    .form-group input {
        width: 100%;
        padding: clamp(10px, 2.5vw, 14px);
        border: 2px solid var(--ctp-surface0);
        border-radius: 8px;
        font-size: clamp(0.9rem, 2vw, 1rem);
        box-sizing: border-box;
        background: var(--ctp-base);
        color: var(--ctp-text);
        transition: all 0.3s ease;
    }

    .form-group input:focus {
        border-color: var(--ctp-flamingo);
        outline: none;
        box-shadow: 0 0 0 3px rgba(242, 205, 205, 0.25);
    }

    .login-btn {
        width: 100%;
        background: linear-gradient(135deg, var(--ctp-flamingo), var(--ctp-maroon));
        color: var(--ctp-crust);
        border: none;
        padding: clamp(12px, 3vw, 18px);
        border-radius: 8px;
        font-size: clamp(1rem, 2.5vw, 1.2rem);
        cursor: pointer;
        font-weight: 600;
        transition: all 0.3s ease;
    }

    .login-btn:hover {
        background: linear-gradient(135deg, var(--ctp-maroon), var(--ctp-pink));
        transform: translateY(-1px);
        box-shadow: 0 8px 25px rgba(242, 205, 205, 0.3);
    }

    .login-btn:disabled {
        background: var(--ctp-surface0);
        cursor: not-allowed;
        transform: none;
        box-shadow: none;
        color: var(--ctp-subtext0);
    }

    /* Dashboard styles */
    .dashboard-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: clamp(20px, 4vw, 40px);
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
    }

    .admin-logo {
        height: clamp(40px, 6vw, 60px);
        width: auto;
        border-radius: 6px;
        box-shadow: 0 3px 8px rgba(0, 0, 0, 0.2);
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
        gap: 15px;
        flex-wrap: wrap;
    }

    .admin-info h1 {
        color: var(--ctp-text);
        margin: 0;
        font-size: clamp(1.8rem, 4vw, 2.5rem);
        font-weight: 600;
    }

    .logout-btn {
        background: linear-gradient(135deg, var(--ctp-red), var(--ctp-maroon));
        color: var(--ctp-crust);
        border: none;
        padding: clamp(8px, 2vw, 12px) clamp(12px, 3vw, 20px);
        border-radius: 8px;
        cursor: pointer;
        font-size: clamp(0.85rem, 1.8vw, 1rem);
        font-weight: 500;
        transition: all 0.3s ease;
    }

    .logout-btn:hover {
        background: linear-gradient(135deg, var(--ctp-maroon), var(--ctp-pink));
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(243, 139, 168, 0.3);
    }

    /* Navigation tabs */
    .nav-tabs {
        display: flex;
        gap: clamp(4px, 0.8vw, 10px);
        margin-bottom: clamp(18px, 3vw, 32px);
        border-bottom: 2px solid var(--ctp-surface0);
        overflow-x: auto;
        scroll-snap-type: x proximity;
        -webkit-overflow-scrolling: touch;
        /* The tab strip is the primary navigation - keep it in reach */
        position: sticky;
        top: 0;
        z-index: 5;
        background: linear-gradient(var(--ctp-base) 70%, transparent);
        padding-bottom: 2px;
    }

    .nav-tabs::-webkit-scrollbar {
        height: 4px;
    }

    .tab-btn {
        background: none;
        border: none;
        padding: clamp(10px, 2vw, 15px) clamp(12px, 2vw, 20px);
        cursor: pointer;
        border-bottom: 3px solid transparent;
        font-size: clamp(0.85rem, 1.7vw, 1rem);
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
        background: var(--ctp-red);
        color: var(--ctp-crust);
        border-radius: 12px;
        padding: 3px 8px;
        font-size: clamp(10px, 1.5vw, 12px);
        margin-left: 8px;
        font-weight: 600;
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
            linear-gradient(180deg, rgba(205, 214, 244, 0.04), transparent 60%),
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

    .stat-number {
        font-size: clamp(2.5rem, 8vw, 4rem);
        font-weight: 700;
        color: var(--ctp-flamingo);
        margin: 15px 0;
    }

    .card h3 {
        color: var(--ctp-text);
        margin-bottom: 15px;
        font-size: clamp(1.1rem, 2.5vw, 1.3rem);
    }

    .card-btn {
        background: linear-gradient(135deg, var(--ctp-flamingo), var(--ctp-maroon));
        color: white;
        border: none;
        padding: clamp(8px, 2vw, 12px) clamp(16px, 3vw, 24px);
        border-radius: 8px;
        cursor: pointer;
        font-size: clamp(0.85rem, 1.8vw, 1rem);
        font-weight: 500;
        transition: all 0.3s ease;
    }

    .card-btn:hover {
        background: linear-gradient(135deg, var(--ctp-maroon), var(--ctp-pink));
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(31, 111, 235, 0.3);
    }

    /* Status Filters */
    .status-filters {
        display: flex;
        gap: clamp(8px, 2vw, 15px);
        margin-bottom: clamp(15px, 3vw, 25px);
        flex-wrap: wrap;
    }

    .filter-btn {
        background: var(--ctp-crust);
        border: 2px solid var(--ctp-surface0);
        color: var(--ctp-subtext0);
        padding: clamp(6px, 1.5vw, 10px) clamp(12px, 3vw, 18px);
        border-radius: 20px;
        cursor: pointer;
        font-size: clamp(0.8rem, 1.8vw, 0.95rem);
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
        color: white;
        font-weight: 600;
    }

    /* Loans grid */
    .loans-grid {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(450px, 1fr));
        gap: clamp(15px, 3vw, 25px);
        margin-top: 20px;
    }

    .loan-card {
        background: var(--ctp-crust);
        border-radius: 12px;
        padding: clamp(18px, 4vw, 25px);
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
        border-left: 5px solid var(--ctp-flamingo);
        border: 1px solid var(--ctp-surface0);
        transition: all 0.3s ease;
    }

    .loan-card:hover {
        transform: translateY(-2px);
        box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
        border-color: var(--ctp-flamingo);
    }

    .loan-card.pending {
        border-left-color: #f7b955;
        background: rgba(247, 185, 85, 0.08);
        border: 1px solid rgba(247, 185, 85, 0.2);
    }

    .loan-card.overdue {
        border-left-color: #f85149;
        background: rgba(248, 81, 73, 0.05);
    }

    /* --- printer cards in the admin dashboard --- */
    .printer-admin-grid {
        display: grid;
        /* min() stops a single column overflowing a narrow phone; the 460px
           cap stops three cards stretching absurdly wide on a large monitor */
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 460px));
        justify-content: center;
        gap: 14px;
    }

    .printer-admin-card {
        --accent: var(--ctp-overlay0);
        background:
            linear-gradient(180deg, rgba(205, 214, 244, 0.035), transparent 55%),
            var(--ctp-mantle);
        border: 1px solid var(--ctp-surface0);
        border-left: 4px solid var(--accent);
        border-radius: var(--radius-lg);
        padding: 16px;
        box-shadow: var(--shadow-sm);
        transition:
            transform var(--normal) var(--ease),
            box-shadow var(--normal) var(--ease);
        animation: rise var(--slow) var(--ease) both;
        animation-delay: calc(var(--i, 0) * 70ms);
    }

    .printer-admin-card:hover {
        transform: translateY(-3px);
        box-shadow: var(--shadow);
    }

    .printer-admin-card.running { --accent: var(--ctp-green); }
    .printer-admin-card.failed { --accent: var(--ctp-red); }
    .printer-admin-card.paused { --accent: var(--ctp-yellow); }
    .printer-admin-card.finished { --accent: var(--ctp-blue); }
    .printer-admin-card.idle { --accent: var(--ctp-sapphire); }
    .printer-admin-card.offline { opacity: 0.62; }

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

    .pa-state.running { background: var(--ctp-green); color: var(--ctp-crust); }
    .pa-state.failed { background: var(--ctp-red); color: var(--ctp-crust); }
    .pa-state.paused { background: var(--ctp-yellow); color: var(--ctp-crust); }
    .pa-state.finished { background: var(--ctp-blue); color: var(--ctp-crust); }

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
        transition: transform var(--slow) var(--ease);
    }

    .printer-admin-card:hover .pa-camera img {
        transform: scale(1.03);
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

    .pa-stop {
        margin-top: 12px;
        width: 100%;
        min-height: 44px;
        background: var(--ctp-red);
        color: var(--ctp-crust);
        border: none;
        border-radius: var(--radius-sm);
        font-weight: 650;
        font-size: 0.9rem;
        cursor: pointer;
        transition:
            background-color var(--fast) var(--ease),
            transform var(--fast) var(--ease);
    }

    .pa-stop:hover { background: var(--ctp-maroon); transform: translateY(-1px); }
    .pa-stop:active { transform: none; }
    .pa-stop:disabled { opacity: 0.6; cursor: not-allowed; }

    .pa-last,
    .pa-offline {
        margin: 8px 0 0;
        font-size: 0.75rem;
        color: var(--ctp-overlay0);
    }

    .pa-warning {
        background: rgba(250, 179, 135, 0.12);
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
        background: var(--ctp-base);
        border: 1px solid var(--ctp-surface1);
        border-radius: 6px;
        padding: 8px;
        color: var(--ctp-text);
    }

    .pa-fix,
    .pa-save {
        background: var(--ctp-peach);
        color: var(--ctp-crust);
        border: none;
        border-radius: 6px;
        padding: 8px 14px;
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
        cursor: pointer;
        min-height: 40px;
    }

    .calendar-link {
        color: var(--ctp-blue);
        text-decoration: none;
        margin-left: 6px;
    }

    /* --- booking calendar --- */
    .cal-toolbar {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
        margin: 14px 0;
    }

    .cal-nav {
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .cal-nav button {
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        border: none;
        border-radius: var(--radius-sm);
        padding: 8px 12px;
        min-height: 40px;
        cursor: pointer;
        font-family: inherit;
        transition: background-color var(--fast) var(--ease);
    }

    .cal-nav button:hover { background: var(--ctp-surface1); }

    .cal-label {
        color: var(--ctp-subtext0);
        font-size: 0.9rem;
        margin-left: 6px;
    }

    .cal-add,
    .cal-save {
        background: linear-gradient(135deg, var(--ctp-mauve), var(--ctp-lavender));
        color: var(--ctp-crust);
        border: none;
        border-radius: var(--radius-sm);
        padding: 10px 18px;
        min-height: 42px;
        font-weight: 650;
        font-family: inherit;
        cursor: pointer;
        box-shadow: var(--shadow-sm);
        transition: transform var(--fast) var(--ease), filter var(--fast) var(--ease);
    }

    .cal-add:hover,
    .cal-save:hover { transform: translateY(-1px); filter: brightness(1.06); }
    .cal-save:disabled { opacity: 0.6; cursor: not-allowed; }

    .cal-form,
    .cal-details {
        background: var(--ctp-mantle);
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        padding: 16px;
        margin-bottom: 14px;
        box-shadow: var(--shadow-sm);
        animation: rise var(--normal) var(--ease) both;
    }

    .cal-form-hint {
        margin: 0 0 10px;
        font-size: 0.82rem;
        color: var(--ctp-subtext0);
    }

    .cal-form-row {
        display: flex;
        gap: 12px;
        flex-wrap: wrap;
    }

    .cal-form label,
    .cal-form-full {
        display: flex;
        flex-direction: column;
        gap: 4px;
        font-size: 0.82rem;
        color: var(--ctp-subtext0);
        flex: 1;
        min-width: 140px;
        margin-bottom: 10px;
    }

    .cal-form input {
        background: var(--ctp-base);
        border: 1px solid var(--ctp-surface1);
        border-radius: 6px;
        padding: 9px;
        color: var(--ctp-text);
        font-size: 0.95rem;
        font-family: inherit;
    }

    .cal-details h3 {
        margin: 0 0 8px;
        color: var(--ctp-mauve);
    }

    .cal-details p {
        margin: 4px 0;
        font-size: 0.9rem;
    }

    .cal-details-actions {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
        margin-top: 12px;
    }

    .cal-delete {
        background: var(--ctp-red);
        color: var(--ctp-crust);
        border: none;
        border-radius: 6px;
        padding: 9px 14px;
        min-height: 40px;
        font-weight: 650;
        cursor: pointer;
        font-family: inherit;
    }

    .cal-delete:hover { background: var(--ctp-maroon); }

    .cal-close {
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        border: none;
        border-radius: 6px;
        padding: 9px 14px;
        min-height: 40px;
        cursor: pointer;
        font-family: inherit;
    }

    .cal {
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        overflow: hidden;
        background: var(--ctp-mantle);
        box-shadow: var(--shadow-sm);
        margin-bottom: 18px;
    }

    .cal-head,
    .cal-body {
        display: grid;
        grid-template-columns: 56px repeat(7, minmax(80px, 1fr));
    }

    .cal-head { border-bottom: 1px solid var(--ctp-surface0); }

    .cal-day-head {
        text-align: center;
        padding: 8px 2px;
        border-left: 1px solid var(--ctp-surface0);
        display: flex;
        flex-direction: column;
    }

    .cal-day-name {
        font-size: 0.72rem;
        color: var(--ctp-subtext0);
        text-transform: uppercase;
    }

    .cal-day-num {
        font-size: 1.05rem;
        font-weight: 650;
    }

    .cal-day-head.today .cal-day-num {
        color: var(--ctp-crust);
        background: var(--ctp-mauve);
        border-radius: 50%;
        width: 28px;
        height: 28px;
        line-height: 28px;
        margin: 2px auto 0;
    }

    .cal-body {
        position: relative;
        max-height: 58vh;
        overflow-y: auto;
    }

    .cal-gutter { border-right: 1px solid var(--ctp-surface0); }

    .cal-time {
        height: 46px;
        font-size: 0.68rem;
        color: var(--ctp-overlay0);
        text-align: right;
        padding-right: 6px;
        transform: translateY(-6px);
    }

    .cal-col {
        position: relative;
        border-left: 1px solid var(--ctp-surface0);
    }

    .cal-col.today { background: rgba(203, 166, 247, 0.06); }

    .cal-cell {
        display: block;
        width: 100%;
        height: 46px;
        border: none;
        border-bottom: 1px solid var(--ctp-surface0);
        background: transparent;
        cursor: pointer;
        padding: 0;
        transition: background-color var(--fast) var(--ease);
    }

    .cal-cell:hover {
        background: rgba(203, 166, 247, 0.16);
        box-shadow: inset 0 0 0 1px rgba(203, 166, 247, 0.35);
    }

    .cal-block {
        position: absolute;
        left: 3px;
        right: 3px;
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
        font-size: 0.68rem;
        line-height: 1.15;
        font-family: inherit;
        box-shadow: var(--shadow-sm);
        transition: transform var(--fast) var(--ease), filter var(--fast) var(--ease);
    }

    .cal-block:hover {
        filter: brightness(1.08);
        transform: translateY(-1px);
        z-index: 2;
    }

    .cal-block-time { font-weight: 700; }
    .cal-block-name { font-weight: 650; }

    .cal-block-purpose {
        opacity: 0.85;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .cal-list-title {
        font-size: 1rem;
        color: var(--ctp-subtext0);
        margin: 18px 0 10px;
    }

    @media (max-width: 700px) {
        .cal { overflow-x: auto; }

        .cal-head,
        .cal-body {
            grid-template-columns: 42px repeat(7, minmax(72px, 1fr));
        }

        .cal-block-purpose { display: none; }
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
        border-left: 4px solid var(--ctp-mauve);
        border-radius: 8px;
        padding: 12px 14px;
        flex-wrap: wrap;
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

    .loan-card.rejected {
        background: rgba(124, 139, 154, 0.1);
        border-left-color: #7c8b9a;
        opacity: 0.8;
    }

    .loan-card.return-pending {
        border-left-color: #f7b955;
        background: rgba(247, 185, 85, 0.05);
    }

    .loan-card.lost-missing {
        border-left-color: #fd7e14;
        background: rgba(253, 126, 20, 0.05);
    }

    .loan-card.archived {
        border-left-color: var(--ctp-overlay0);
        background: rgba(108, 112, 134, 0.05);
        opacity: 0.9;
    }

    /* History View Styles */
    .history-search-container {
        background: var(--ctp-crust);
        padding: 20px;
        border-radius: 12px;
        margin-bottom: 20px;
        border: 1px solid var(--ctp-surface0);
        box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
    }

    .history-search-container label {
        display: block;
        margin-bottom: 10px;
        font-weight: 600;
        color: var(--ctp-text);
        font-size: 1rem;
    }

    .history-search-input {
        width: 100%;
        padding: 12px 16px;
        border: 2px solid var(--ctp-surface0);
        border-radius: 8px;
        font-size: 1rem;
        box-sizing: border-box;
        background: var(--ctp-base);
        color: var(--ctp-text);
        transition: all 0.3s ease;
    }

    .history-search-input:focus {
        border-color: var(--ctp-flamingo);
        outline: none;
        box-shadow: 0 0 0 3px rgba(242, 205, 205, 0.25);
    }

    .search-results-info {
        margin-top: 10px;
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
        margin-bottom: 15px;
    }

    .clear-search-btn-large {
        background: linear-gradient(135deg, var(--ctp-sapphire), var(--ctp-blue));
        color: var(--ctp-crust);
        border: none;
        padding: 10px 20px;
        border-radius: 8px;
        font-size: 1rem;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.3s ease;
    }

    .clear-search-btn-large:hover {
        background: linear-gradient(135deg, var(--ctp-blue), var(--ctp-lavender));
        transform: translateY(-1px);
        box-shadow: 0 4px 12px rgba(116, 199, 236, 0.3);
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
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
        border: 1px solid var(--ctp-surface0);
        transition: all 0.3s ease;
        position: relative;
    }

    .history-item:hover {
        transform: translateY(-2px);
        box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
        border-color: var(--ctp-flamingo);
    }

    .history-item.approved {
        border-left: 5px solid var(--ctp-green);
    }

    .history-item.returned {
        border-left: 5px solid var(--ctp-teal);
    }

    .history-item.not_found {
        border-left: 5px solid var(--ctp-red);
    }

    .history-item.denied {
        border-left: 5px solid var(--ctp-overlay0);
    }

    .history-item.pending {
        border-left: 5px solid #f7b955;
    }

    .history-timeline {
        display: flex;
        flex-direction: column;
        align-items: center;
        width: 60px;
        min-height: 100%;
        padding: 20px 0;
        position: relative;
    }

    .timeline-dot {
        width: 16px;
        height: 16px;
        border-radius: 50%;
        border: 3px solid var(--ctp-surface0);
        z-index: 2;
        margin-top: 5px;
    }

    .timeline-dot.approved {
        background: var(--ctp-green);
        border-color: var(--ctp-green);
    }

    .timeline-dot.returned {
        background: var(--ctp-teal);
        border-color: var(--ctp-teal);
    }

    .timeline-dot.not_found {
        background: var(--ctp-red);
        border-color: var(--ctp-red);
    }

    .timeline-dot.denied {
        background: var(--ctp-overlay0);
        border-color: var(--ctp-overlay0);
    }

    .timeline-dot.pending {
        background: #f7b955;
        border-color: #f7b955;
        animation: pulse 2s infinite;
    }

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
        padding: 20px 25px;
    }

    .history-header {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        margin-bottom: 20px;
        gap: 15px;
        flex-wrap: wrap;
    }

    .history-title-section {
        display: flex;
        align-items: flex-start;
        gap: 15px;
        flex: 1;
    }

    .history-title-section h3 {
        color: var(--ctp-text);
        margin: 0;
        font-size: clamp(1.1rem, 2.5vw, 1.3rem);
        font-weight: 600;
        line-height: 1.2;
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

    .history-details {
        margin-top: 15px;
    }

    .detail-row {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
        gap: 20px;
    }

    .detail-group {
        display: flex;
        flex-direction: column;
        gap: 8px;
    }

    .detail-group p {
        margin: 0;
        color: var(--ctp-subtext0);
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
    }

    .detail-group strong {
        color: var(--ctp-text);
        font-weight: 600;
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
    }

    .loan-title-section h3 {
        margin: 0;
        color: var(--ctp-text);
        font-size: clamp(1.1rem, 2.5vw, 1.3rem);
        flex: 1;
        font-weight: 600;
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
        font-size: clamp(10px, 1.5vw, 12px);
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.5px;
    }

    .status-badge.pending {
        background: rgba(247, 185, 85, 0.2);
        color: #f7b955;
        border: 1px solid rgba(247, 185, 85, 0.3);
    }

    .status-badge.approved {
        background: rgba(46, 160, 67, 0.2);
        color: #3fb950;
        border: 1px solid rgba(46, 160, 67, 0.3);
    }

    .status-badge.denied {
        background: rgba(124, 139, 154, 0.2);
        color: #7c8b9a;
        border: 1px solid rgba(124, 139, 154, 0.3);
    }

    .status-badge.return-pending {
        background: rgba(247, 185, 85, 0.2);
        color: #f7b955;
        border: 1px solid rgba(247, 185, 85, 0.3);
    }

    .status-badge.not-found {
        background: rgba(253, 126, 20, 0.2);
        color: #fd7e14;
        border: 1px solid rgba(253, 126, 20, 0.3);
    }

    .status-badge.archived {
        background: rgba(108, 112, 134, 0.2);
        color: var(--ctp-overlay0);
        border: 1px solid rgba(108, 112, 134, 0.3);
    }

    .overdue-badge {
        background: #f85149;
        color: white;
        padding: 4px 10px;
        border-radius: 12px;
        font-size: clamp(10px, 1.5vw, 12px);
        font-weight: 600;
        animation: pulse 2s infinite;
    }

    @keyframes pulse {
        0% { opacity: 1; box-shadow: 0 0 0 0 rgba(248, 81, 73, 0.7); }
        50% { opacity: 0.8; box-shadow: 0 0 0 8px rgba(248, 81, 73, 0); }
        100% { opacity: 1; box-shadow: 0 0 0 0 rgba(248, 81, 73, 0); }
    }

    .loan-details p {
        margin: clamp(6px, 1.5vw, 10px) 0;
        color: var(--ctp-subtext0);
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
    }

    .loan-details strong {
        color: var(--ctp-text);
        font-weight: 600;
    }

    .clickable-phone {
        color: var(--ctp-blue);
        cursor: pointer;
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

    .clickable-phone:focus {
        outline: 2px solid var(--ctp-flamingo);
        outline-offset: 2px;
        background-color: var(--ctp-surface0);
    }

    .photo-section {
        margin: 15px 0;
        padding: 12px;
        background: #161b22;
        border-radius: 8px;
        border: 1px solid var(--ctp-surface0);
    }

    .item-image {
        width: clamp(70px, 15vw, 90px);
        height: clamp(70px, 15vw, 90px);
        border-radius: 8px;
        overflow: hidden;
        border: 2px solid var(--ctp-surface0);
        flex-shrink: 0;
    }

    .item-image img {
        width: 100%;
        height: 100%;
        object-fit: cover;
    }

    .item-image.placeholder {
        display: flex;
        align-items: center;
        justify-content: center;
        background: #161b22;
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

    .approve-btn, .deny-btn, .extend-btn, .not-found-btn {
        border: none;
        padding: clamp(6px, 1.5vw, 10px) clamp(12px, 3vw, 18px);
        border-radius: 8px;
        cursor: pointer;
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
        font-weight: 500;
        transition: all 0.3s ease;
    }

    .approve-btn {
        background: linear-gradient(135deg, #238636, #2ea043);
        color: white;
    }

    .approve-btn:hover {
        background: linear-gradient(135deg, #2ea043, #3fb950);
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(46, 160, 67, 0.3);
    }

    .deny-btn {
        background: linear-gradient(135deg, #da3633, #f85149);
        color: white;
    }

    .deny-btn:hover {
        background: linear-gradient(135deg, #f85149, #ff7b72);
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(248, 81, 73, 0.3);
    }

    .extend-btn {
        background: linear-gradient(135deg, var(--ctp-flamingo), var(--ctp-maroon));
        color: white;
    }

    .extend-btn:hover {
        background: linear-gradient(135deg, var(--ctp-maroon), var(--ctp-pink));
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(31, 111, 235, 0.3);
    }

    .not-found-btn {
        background: linear-gradient(135deg, #fd7e14, #ff8c00);
        color: white;
    }

    .not-found-btn:hover {
        background: linear-gradient(135deg, #ff8c00, #ffa500);
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(253, 126, 20, 0.3);
    }

    .extend-controls {
        display: flex;
        flex-direction: column;
        gap: 10px;
        margin-bottom: 10px;
        width: 100%;
    }

    .extend-inputs {
        display: flex;
        align-items: center;
        gap: clamp(6px, 1.5vw, 10px);
        flex-wrap: wrap;
    }

    .extend-inputs input {
        width: clamp(50px, 12vw, 70px);
        padding: clamp(4px, 1vw, 8px);
        border: 2px solid var(--ctp-surface0);
        border-radius: 6px;
        text-align: center;
        background: var(--ctp-base);
        color: var(--ctp-text);
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
    }

    .extend-inputs input:focus {
        border-color: var(--ctp-flamingo);
        outline: none;
    }

    .extend-inputs span {
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
        color: var(--ctp-subtext0);
    }

    .return-section {
        background: rgba(247, 185, 85, 0.1);
        border: 1px solid rgba(247, 185, 85, 0.3);
        border-radius: 8px;
        padding: 15px;
        margin-top: 15px;
    }

    .return-notice {
        color: #f7b955;
        font-weight: 600;
        margin-bottom: 10px;
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
    }

    .return-actions {
        display: flex;
        gap: 10px;
        flex-wrap: wrap;
    }

    .found-section {
        background: rgba(166, 227, 161, 0.1);
        border: 1px solid rgba(166, 227, 161, 0.3);
        border-radius: 8px;
        padding: 15px;
        margin-top: 15px;
    }

    .found-notice {
        color: var(--ctp-green);
        font-weight: 600;
        margin-bottom: 10px;
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
    }

    .found-actions {
        display: flex;
        gap: 10px;
        flex-wrap: wrap;
    }

    .found-btn {
        background: var(--ctp-green);
        color: var(--ctp-base);
        border: none;
        padding: clamp(8px, 2vw, 12px) clamp(14px, 2.5vw, 20px);
        border-radius: 6px;
        cursor: pointer;
        font-size: clamp(0.8rem, 1.6vw, 0.9rem);
        font-weight: 600;
        transition: all 0.3s ease;
        box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
    }

    .found-btn:hover {
        background: #94d3a2;
        transform: translateY(-2px);
        box-shadow: 0 4px 12px rgba(166, 227, 161, 0.3);
    }

    .found-btn:disabled {
        opacity: 0.6;
        cursor: not-allowed;
        transform: none;
    }

    .overdue-text {
        color: #f85149;
        font-weight: 600;
    }

    .return-date {
        color: var(--ctp-teal);
        font-weight: 500;
        margin: 8px 0;
    }

    .overdue-returned {
        margin: 8px 0;
    }

    .overdue-mark {
        color: #f7b955;
        font-weight: 600;
        background: rgba(247, 185, 85, 0.15);
        padding: 2px 6px;
        border-radius: 4px;
        border: 1px solid rgba(247, 185, 85, 0.3);
    }

    .admin-actions {
        margin-top: 30px;
        padding: clamp(18px, 4vw, 25px);
        background: var(--ctp-crust);
        border-radius: 12px;
        border: 1px solid var(--ctp-surface0);
    }

    .cleanup-btn, .export-btn {
        border: none;
        padding: clamp(10px, 2.5vw, 14px) clamp(16px, 3vw, 24px);
        border-radius: 8px;
        cursor: pointer;
        font-size: clamp(0.85rem, 1.8vw, 1rem);
        font-weight: 500;
        transition: all 0.3s ease;
        margin-right: 10px;
        margin-bottom: 10px;
    }

    .cleanup-btn {
        background: linear-gradient(135deg, #6c757d, #495057);
        color: white;
    }

    .cleanup-btn:hover {
        background: linear-gradient(135deg, #495057, #343a40);
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(108, 117, 125, 0.3);
    }

    .export-btn {
        background: linear-gradient(135deg, #238636, #2ea043);
        color: white;
    }

    .export-btn:hover {
        background: linear-gradient(135deg, #2ea043, #3fb950);
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(46, 160, 67, 0.3);
    }

    .subtitle-text {
        color: var(--ctp-subtext0);
        font-style: italic;
        margin-bottom: 20px;
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
    }

    .credits-footer {
        background: var(--ctp-crust);
        padding: clamp(15px, 3vw, 20px);
        text-align: center;
        border-top: 1px solid var(--ctp-surface0);
        margin-top: 40px;
        color: var(--ctp-subtext0);
        font-size: clamp(12px, 2vw, 14px);
    }

    .credits-footer a {
        color: var(--ctp-flamingo);
        text-decoration: none;
    }

    .credits-footer a:hover {
        text-decoration: underline;
        color: #79c0ff;
    }

    .loan-actions input[type="date"] {
        padding: clamp(6px, 1.5vw, 10px);
        border: 2px solid var(--ctp-surface0);
        border-radius: 6px;
        flex: 1;
        background: var(--ctp-base);
        color: var(--ctp-text);
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
    }

    .loan-actions input[type="date"]:focus {
        border-color: var(--ctp-flamingo);
        outline: none;
    }

    .no-items {
        text-align: center;
        color: var(--ctp-subtext0);
        font-style: italic;
        padding: clamp(30px, 6vw, 50px);
        font-size: clamp(1rem, 2vw, 1.1rem);
    }

    /* Enhanced Responsive Design */
    @media (max-width: 768px) {
        .admin-container {
            padding: 15px;
        }

        .dashboard-header {
            flex-direction: column;
            gap: 15px;
            text-align: center;
        }

        .nav-tabs {
            justify-content: flex-start;
            overflow-x: auto;
            -webkit-overflow-scrolling: touch;
        }

        .loans-grid {
            grid-template-columns: 1fr;
        }

        .loan-card {
            padding: 20px;
        }

        .loan-title-section {
            flex-direction: column;
            gap: 10px;
        }

        .item-image {
            align-self: flex-start;
        }

        .loan-actions {
            flex-direction: column;
            align-items: stretch;
        }

        .loan-actions > * {
            width: 100%;
        }

        .extend-inputs {
            justify-content: space-between;
        }

        .overview-cards {
            grid-template-columns: 1fr;
        }

        .status-filters {
            justify-content: center;
        }

        .admin-actions {
            text-align: center;
        }

        .cleanup-btn, .export-btn {
            width: 100%;
            margin: 5px 0;
        }
    }

    /* Extra small screens (phones in portrait) */
    @media (max-width: 480px) {
        .admin-container {
            padding: 10px;
        }

        .login-form {
            padding: 25px;
            margin: 0 10px;
        }

        .tab-btn {
            padding: 12px 15px;
            font-size: 0.9rem;
        }

        .loan-card {
            padding: 15px;
        }

        .status-filters {
            gap: 5px;
        }

        .filter-btn {
            padding: 6px 12px;
            font-size: 0.8rem;
        }
    }

    /* Large screens */
    @media (min-width: 1200px) {
        .loans-grid {
            grid-template-columns: repeat(auto-fill, minmax(500px, 1fr));
        }

        .overview-cards {
            grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
        }
    }

    /* Extra large screens */
    @media (min-width: 1600px) {
        .admin-container {
            max-width: 1800px;
        }

        .loans-grid {
            grid-template-columns: repeat(auto-fill, minmax(550px, 1fr));
        }
    }

    /* Admin Management Styles */
    .form-container {
        padding: 20px;
        max-width: 600px;
        margin: 0 auto;
    }

    .form-card {
        background: var(--ctp-surface0);
        border-radius: 12px;
        padding: 24px;
        border: 1px solid var(--ctp-surface1);
        box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
    }

    .form-group {
        margin-bottom: 20px;
    }

    .form-group label {
        display: block;
        margin-bottom: 8px;
        font-weight: 500;
        color: var(--ctp-text);
    }

    .form-group input[type="text"],
    .form-group input[type="password"] {
        width: 100%;
        padding: 12px 16px;
        background: var(--ctp-base);
        border: 2px solid var(--ctp-surface1);
        border-radius: 8px;
        color: var(--ctp-text);
        font-size: 1rem;
        transition: all 0.2s ease;
        box-sizing: border-box;
    }

    .form-group input[type="text"]:focus,
    .form-group input[type="password"]:focus {
        outline: none;
        border-color: var(--ctp-blue);
        box-shadow: 0 0 0 3px rgba(137, 180, 250, 0.1);
    }

    .checkbox-label {
        display: flex !important;
        align-items: center;
        gap: 12px;
        cursor: pointer;
    }

    .checkbox-label input[type="checkbox"] {
        width: auto !important;
        margin: 0;
    }

    .form-actions {
        display: flex;
        gap: 12px;
        margin-top: 24px;
    }

    .submit-btn {
        background: linear-gradient(135deg, var(--ctp-blue), var(--ctp-sapphire));
        color: var(--ctp-base);
        border: none;
        padding: 12px 24px;
        border-radius: 8px;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.2s ease;
        flex: 1;
    }

    .submit-btn:hover:not(:disabled) {
        transform: translateY(-2px);
        box-shadow: 0 4px 12px rgba(137, 180, 250, 0.3);
    }

    .submit-btn:disabled {
        opacity: 0.6;
        cursor: not-allowed;
        transform: none;
    }

    .cancel-btn {
        background: var(--ctp-overlay0);
        color: var(--ctp-text);
        border: none;
        padding: 12px 24px;
        border-radius: 8px;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.2s ease;
    }

    .cancel-btn:hover {
        background: #7c7f93;
        transform: translateY(-1px);
    }

    .admin-management-container {
        padding: 20px;
        max-width: 1000px;
        margin: 0 auto;
    }

    .management-section {
        background: var(--ctp-surface0);
        border-radius: 12px;
        padding: 24px;
        margin-bottom: 24px;
        border: 1px solid var(--ctp-surface1);
    }

    .section-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: 20px;
    }

    .section-header h3 {
        margin: 0;
        color: var(--ctp-text);
        font-size: 1.2rem;
    }

    .toggle-btn {
        background: var(--ctp-blue);
        color: var(--ctp-base);
        border: none;
        padding: 8px 16px;
        border-radius: 6px;
        font-weight: 500;
        cursor: pointer;
        transition: all 0.2s ease;
    }

    .toggle-btn:hover {
        background: var(--ctp-sapphire);
        transform: translateY(-1px);
    }

    .refresh-btn {
        background: var(--ctp-green);
        color: var(--ctp-base);
        border: none;
        padding: 8px 16px;
        border-radius: 6px;
        font-weight: 500;
        cursor: pointer;
        transition: all 0.2s ease;
    }

    .refresh-btn:hover {
        background: var(--ctp-teal);
        transform: translateY(-1px);
    }

    .admin-list {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
        gap: 16px;
    }

    .admin-card {
        background: var(--ctp-base);
        border-radius: 8px;
        padding: 20px;
        border: 1px solid var(--ctp-surface1);
        transition: all 0.2s ease;
        display: flex;
        flex-direction: column;
        justify-content: space-between;
    }

    .admin-card:hover {
        border-color: var(--ctp-blue);
        box-shadow: 0 4px 12px rgba(137, 180, 250, 0.1);
    }

    .admin-info h4 {
        margin: 0 0 8px 0;
        color: var(--ctp-text);
        font-size: 1.1rem;
    }

    .admin-username {
        margin: 4px 0;
        color: var(--ctp-blue);
        font-weight: 500;
    }

    .admin-role {
        margin: 8px 0;
        font-weight: 600;
    }

    .admin-date {
        margin: 8px 0 0 0;
        color: var(--ctp-overlay0);
        font-size: 0.9rem;
    }

    .admin-actions {
        display: flex;
        justify-content: flex-end;
        align-items: center;
        margin-top: 16px;
        padding-top: 12px;
        border-top: 1px solid var(--ctp-surface1);
    }

    .delete-admin-btn {
        background: var(--ctp-red);
        color: var(--ctp-base);
        border: none;
        padding: 6px 12px;
        border-radius: 6px;
        font-weight: 500;
        cursor: pointer;
        transition: all 0.2s ease;
        font-size: 0.9rem;
    }

    .delete-admin-btn:hover {
        background: var(--ctp-maroon);
        transform: translateY(-1px);
    }

    .current-user-badge {
        background: var(--ctp-green);
        color: var(--ctp-base);
        padding: 4px 8px;
        border-radius: 4px;
        font-size: 0.8rem;
        font-weight: 600;
    }

    .protected-user-badge {
        background: var(--ctp-mauve);
        color: var(--ctp-base);
        padding: 4px 8px;
        border-radius: 4px;
        font-size: 0.8rem;
        font-weight: 600;
    }

    .danger-zone {
        border-color: var(--ctp-red) !important;
        background: rgba(243, 139, 168, 0.1);
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
        color: var(--ctp-base);
        border: none;
        padding: 10px 20px;
        border-radius: 6px;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.2s ease;
        white-space: nowrap;
        margin-left: 16px;
    }

    .danger-btn:hover {
        background: var(--ctp-maroon);
        transform: translateY(-1px);
    }

    .link-btn {
        background: none;
        border: none;
        color: var(--ctp-blue);
        cursor: pointer;
        text-decoration: underline;
        font-size: inherit;
    }

    .link-btn:hover {
        color: var(--ctp-sapphire);
    }

    .no-data {
        text-align: center;
        color: var(--ctp-overlay0);
        padding: 20px;
    }

    /* Responsive styles for admin management */
    @media (max-width: 768px) {
        .form-container,
        .admin-management-container {
            padding: 16px;
        }

        .form-actions {
            flex-direction: column;
        }

        .admin-list {
            grid-template-columns: 1fr;
        }

        .danger-item {
            flex-direction: column;
            align-items: flex-start;
            gap: 12px;
        }

        .danger-btn {
            margin-left: 0;
            align-self: stretch;
        }

        .section-header {
            flex-direction: column;
            align-items: flex-start;
            gap: 12px;
        }
    }
</style>