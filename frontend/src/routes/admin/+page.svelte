<script>
    import { onMount } from 'svelte';

    // Authentication state
    let isLoggedIn = false;
    let adminInfo = null;
    
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
    let currentView = 'login'; // login, dashboard, pending, pending-returns, lost-missing, history, lab-view, admin-management, change-password
    let selectedLab = '';
    let selectedFilter = 'all'; // all, borrowed, rejected, returned, pending, not_found
    
    // Data
    let pendingLoans = [];
    let pendingReturns = [];
    let lostMissingItems = [];
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

    onMount(() => {
        // Check if admin is already logged in
        const savedAdmin = localStorage.getItem('adminInfo');
        if (savedAdmin) {
            adminInfo = JSON.parse(savedAdmin);
            isLoggedIn = true;
            currentView = 'dashboard';
            // Load initial data
            loadPendingLoans();
            loadPendingReturns();
            loadLostMissingItems();
        }
    });

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
                const data = await response.json();
                adminInfo = data.admin;
                localStorage.setItem('adminInfo', JSON.stringify(adminInfo));
                isLoggedIn = true;
                currentView = 'dashboard';
                showMessage('Login successful!', 'success');
                loginForm = { username: '', password: '' };
                // Load initial data
                loadPendingLoans();
                loadPendingReturns();
                loadLostMissingItems();
            } else {
                const error = await response.json();
                showMessage(error.error || 'Login failed', 'error');
            }
        } catch (e) {
            // Network error (could be mDNS/name resolution). Offer IP fallback.
            showMessage('Login failed (network). If you are using rrc-inventory.local, mDNS may not be resolving — try the host IP below.', 'error');
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
            const url = `http://${hostOnly}/api/admin/login`;
            const response = await fetch(url, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(loginForm)
            });

            if (response.ok) {
                const data = await response.json();
                adminInfo = data.admin;
                localStorage.setItem('adminInfo', JSON.stringify(adminInfo));
                isLoggedIn = true;
                currentView = 'dashboard';
                showMessage('Login successful (via IP)!', 'success');
                loginForm = { username: '', password: '' };
                // Load initial data
                loadPendingLoans();
                loadPendingReturns();
                loadLostMissingItems();
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
    function logout() {
        localStorage.removeItem('adminInfo');
        isLoggedIn = false;
        adminInfo = null;
        currentView = 'login';
        showMessage('Logged out successfully', 'success');
    }

    // Load pending loans
    async function loadPendingLoans() {
        loading = true;
        try {
            const response = await fetch('/api/admin/loans/pending');
            if (response.ok) {
                pendingLoans = await response.json();
            } else {
                showMessage('Failed to load pending loans', 'error');
            }
        } catch (e) {
            showMessage('Failed to load pending loans', 'error');
        } finally {
            loading = false;
        }
    }

    // Load pending returns
    async function loadPendingReturns() {
        loading = true;
        try {
            const response = await fetch('/api/admin/loans/pending-returns');
            if (response.ok) {
                pendingReturns = await response.json();
            } else {
                showMessage('Failed to load pending returns', 'error');
            }
        } catch (e) {
            showMessage('Failed to load pending returns', 'error');
        } finally {
            loading = false;
        }
    }

    // Load lost/missing items
    async function loadLostMissingItems() {
        loading = true;
        try {
            const response = await fetch('/api/admin/loans/lost-missing');
            if (response.ok) {
                lostMissingItems = await response.json();
            } else {
                showMessage('Failed to load lost/missing items', 'error');
            }
        } catch (e) {
            showMessage('Failed to load lost/missing items', 'error');
        } finally {
            loading = false;
        }
    }

    // Load complete item history (all items chronologically)
    async function loadHistoryItems() {
        loading = true;
        try {
            const response = await fetch('/api/admin/loans/history');
            if (response.ok) {
                historyItems = await response.json();
            } else {
                showMessage('Failed to load item history', 'error');
            }
        } catch (e) {
            showMessage('Failed to load item history', 'error');
        } finally {
            loading = false;
        }
    }

    // Load loans by lab
    async function loadLabLoans(lab, filter = 'all') {
        loading = true;
        try {
            const response = await fetch(`/api/admin/loans/by-lab/${encodeURIComponent(lab)}?status=${filter}`);
            if (response.ok) {
                labLoans = await response.json();
                selectedLab = lab;
                selectedFilter = filter;
            } else {
                showMessage('Failed to load lab loans', 'error');
            }
        } catch (e) {
            showMessage('Failed to load lab loans', 'error');
        } finally {
            loading = false;
        }
    }

    // Approve or deny loan
    async function approveLoan(loanId, action) {
        loading = true;
        try {
            const response = await fetch(`/api/admin/loans/${loanId}/approve`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    action: action,
                    admin_name: adminInfo.name
                })
            });

            if (response.ok) {
                showMessage(`Loan ${action}d successfully!`, 'success');
                if (currentView === 'pending') {
                    loadPendingLoans();
                } else if (currentView === 'lab-view') {
                    loadLabLoans(selectedLab);
                }
            } else {
                const error = await response.json();
                showMessage(error.error || `Failed to ${action} loan`, 'error');
            }
        } catch (e) {
            showMessage(`Failed to ${action} loan`, 'error');
        } finally {
            loading = false;
        }
    }

    // Extend loan
    async function extendLoan(loanId, days, hours) {
        loading = true;
        try {
            const response = await fetch(`/api/admin/loans/${loanId}/extend`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    extend_days: parseInt(days) || 0,
                    extend_hours: parseInt(hours) || 0,
                    admin_name: adminInfo.name
                })
            });

            if (response.ok) {
                showMessage('Loan extended successfully!', 'success');
                if (currentView === 'lab-view') {
                    loadLabLoans(selectedLab, selectedFilter);
                }
            } else {
                const error = await response.json();
                showMessage(error.error || 'Failed to extend loan', 'error');
            }
        } catch (e) {
            showMessage('Failed to extend loan', 'error');
        } finally {
            loading = false;
        }
    }

    // Approve return request
    async function approveReturn(loanId, action = 'approved') {
        loading = true;
        try {
            // Check if admin info is available
            if (!adminInfo || !adminInfo.name) {
                showMessage('Admin authentication required. Please log in again.', 'error');
                return;
            }

            console.log('Approving return:', { loanId, action, adminName: adminInfo.name });

            const response = await fetch(`/api/admin/loans/${loanId}/approve-return`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    action: action,
                    admin_name: adminInfo.name
                })
            });

            const result = await response.json();
            console.log('Return approval response:', result);

            if (response.ok) {
                showMessage(result.message, 'success');
                if (currentView === 'pending-returns') {
                    loadPendingReturns();
                } else if (currentView === 'lab-view') {
                    loadLabLoans(selectedLab, selectedFilter);
                }
            } else {
                console.error('Return approval error:', result);
                showMessage(result.error || 'Failed to process return', 'error');
            }
        } catch (e) {
            console.error('Return approval exception:', e);
            showMessage('Network error. Failed to process return', 'error');
        } finally {
            loading = false;
        }
    }

    // Mark item as found (restore from not_found status)
    async function markAsFound(loanId) {
        loading = true;
        try {
            if (!adminInfo || !adminInfo.name) {
                showMessage('Admin authentication required. Please log in again.', 'error');
                return;
            }

            console.log('Marking item as found:', { loanId, adminName: adminInfo.name });

            const response = await fetch(`/api/admin/loans/${loanId}/mark-found`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    admin_name: adminInfo.name
                })
            });

            const result = await response.json();
            console.log('Mark as found response:', result);

            if (response.ok) {
                showMessage(result.message, 'success');
                // Refresh the lost/missing items view
                if (currentView === 'lost-missing') {
                    loadLostMissingItems();
                }
            } else {
                console.error('Mark as found error:', result);
                showMessage(result.error || 'Failed to mark item as found', 'error');
            }
        } catch (e) {
            console.error('Mark as found exception:', e);
            showMessage('Network error. Failed to mark item as found', 'error');
        } finally {
            loading = false;
        }
    }

    // Cleanup denied loans
    async function cleanupDeniedLoans() {
        loading = true;
        try {
            const response = await fetch('/api/admin/cleanup-denied', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' }
            });

            if (response.ok) {
                const result = await response.json();
                showMessage(`Cleanup completed. ${result.deleted_count} denied loans removed.`, 'success');
            } else {
                const error = await response.json();
                showMessage(error.error || 'Failed to cleanup', 'error');
            }
        } catch (e) {
            showMessage('Failed to cleanup', 'error');
        } finally {
            loading = false;
        }
    }

    // Export data as CSV
    async function exportCSV() {
        try {
            const response = await fetch('/api/admin/export-csv');
            if (response.ok) {
                const blob = await response.blob();
                const url = window.URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = 'robotics_research_centre_loans.csv';
                document.body.appendChild(a);
                a.click();
                document.body.removeChild(a);
                window.URL.revokeObjectURL(url);
                showMessage('Data exported successfully!', 'success');
            } else {
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

    function isOverdue(returnDate) {
        return new Date(returnDate) < new Date();
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

    function showPendingLoans() {
        currentView = 'pending';
        loadPendingLoans();
    }

    function showPendingReturns() {
        currentView = 'pending-returns';
        loadPendingReturns();
    }

    function showLostMissingItems() {
        currentView = 'lost-missing';
        loadLostMissingItems();
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
        
        try {
            const response = await fetch(`/api/admin/list?requesting_username=${adminInfo.username}`);
            if (response.ok) {
                adminList = await response.json();
            } else {
                showMessage('Failed to load admin list', 'error');
            }
        } catch (error) {
            showMessage('Failed to load admin list', 'error');
        }
    }

    async function createAdmin() {
        if (!adminInfo?.is_super_admin) return;
        
        try {
            const response = await fetch('/api/admin/create', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    requesting_username: adminInfo.username,
                    ...createAdminForm
                })
            });

            const result = await response.json();
            
            if (response.ok) {
                showMessage('Admin created successfully', 'success');
                showCreateAdminForm = false;
                createAdminForm = { username: '', password: '', name: '', is_super_admin: false };
                loadAdminList();
            } else {
                showMessage(result.error || 'Failed to create admin', 'error');
            }
        } catch (error) {
            showMessage('Failed to create admin', 'error');
        }
    }

    async function changePassword() {
        if (changePasswordForm.new_password !== changePasswordForm.confirm_password) {
            showMessage('New passwords do not match', 'error');
            return;
        }

        try {
            const response = await fetch('/api/admin/change-password', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    username: adminInfo.username,
                    old_password: changePasswordForm.old_password,
                    new_password: changePasswordForm.new_password
                })
            });

            const result = await response.json();
            
            if (response.ok) {
                showMessage('Password changed successfully', 'success');
                changePasswordForm = { old_password: '', new_password: '', confirm_password: '' };
                currentView = 'dashboard';
            } else {
                showMessage(result.error || 'Failed to change password', 'error');
            }
        } catch (error) {
            showMessage('Failed to change password', 'error');
        }
    }

    async function deleteAllItems() {
        if (!adminInfo?.is_super_admin) return;
        
        if (!confirm('⚠️ WARNING: This will permanently delete ALL items data!\n\nThis action cannot be undone. Are you sure you want to continue?')) {
            return;
        }

        try {
            const response = await fetch('/api/admin/delete-all-items', {
                method: 'DELETE',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    requesting_username: adminInfo.username,
                    confirm_delete: true
                })
            });

            const result = await response.json();
            
            if (response.ok) {
                showMessage(`Successfully deleted ${result.deleted_count} items`, 'success');
            } else {
                showMessage(result.error || 'Failed to delete items', 'error');
            }
        } catch (error) {
            showMessage('Failed to delete items', 'error');
        }
    }

    async function deleteAdmin(adminId, adminName, adminUsername) {
        if (!adminInfo?.is_super_admin) return;

        // Prevent deleting self
        if (adminUsername === adminInfo.username) {
            showMessage('Cannot delete yourself', 'error');
            return;
        }

        // Prevent deleting main super admin
        if (adminUsername === 'Srinath') {
            showMessage('Cannot delete the main super admin account', 'error');
            return;
        }
        
        if (!confirm(`⚠️ WARNING: This will permanently delete the admin account!\n\nAdmin: ${adminName} (@${adminUsername})\n\nThis action cannot be undone. Are you sure you want to continue?`)) {
            return;
        }

        try {
            const response = await fetch(`/api/admin/delete/${adminId}`, {
                method: 'DELETE',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    requesting_username: adminInfo.username
                })
            });

            const result = await response.json();
            
            if (response.ok) {
                showMessage(`Successfully deleted admin: ${result.deleted_admin.name}`, 'success');
                loadAdminList(); // Reload the admin list
            } else {
                showMessage(result.error || 'Failed to delete admin', 'error');
            }
        } catch (error) {
            showMessage('Failed to delete admin', 'error');
        }
    }

    // Reactive statements
    $: if (currentView === 'pending') {
        loadPendingLoans();
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
                    class:active={currentView === 'pending'}
                    on:click={showPendingLoans}
                >
                    Borrow Pending Requests
                    {#if pendingLoans.length > 0}
                        <span class="badge">{pendingLoans.length}</span>
                    {/if}
                </button>
                <button 
                    class="tab-btn" 
                    class:active={currentView === 'pending-returns'}
                    on:click={showPendingReturns}
                >
                    Return Pending Requests
                    {#if pendingReturns.length > 0}
                        <span class="badge">{pendingReturns.length}</span>
                    {/if}
                </button>
                <button 
                    class="tab-btn" 
                    class:active={currentView === 'lost-missing'}
                    on:click={showLostMissingItems}
                >
                    🔍 Lost/Missing Items
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
                            <h3>📋 Pending Requests</h3>
                            <p class="stat-number">{pendingLoans.length}</p>
                            <button class="card-btn" on:click={showPendingLoans}>Review</button>
                        </div>
                        <div class="card">
                            <h3>🔄 Pending Returns</h3>
                            <p class="stat-number">{pendingReturns.length}</p>
                            <button class="card-btn" on:click={showPendingReturns}>Review</button>
                        </div>
                        <div class="card">
                            <h3>🔍 Lost/Missing Items</h3>
                            <p class="stat-number">{lostMissingItems.length}</p>
                            <button class="card-btn" on:click={showLostMissingItems}>Review</button>
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
                        <button class="cleanup-btn" on:click={cleanupDeniedLoans}>
                            🗑️ Cleanup Denied Loans (24h+)
                        </button>
                        <button class="export-btn" on:click={exportCSV}>
                            📊 Export All Data (CSV)
                        </button>
                    </div>
                </div>
            {/if}

            <!-- Pending Loans View -->
            {#if currentView === 'pending'}
                <div class="loans-content">
                    <h2>📋 Pending Loan Requests</h2>
                    {#if loading}
                        <p>Loading pending requests...</p>
                    {:else if pendingLoans.length === 0}
                        <p class="no-items">No pending requests.</p>
                    {:else}
                        <div class="loans-grid">
                            {#each pendingLoans as loan}
                                <div class="loan-card pending">
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
                                            <span class="status-badge pending">Pending</span>
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
                                        <p><strong>Purpose:</strong> {loan.purpose}</p>
                                        <p><strong>Expected Return:</strong> {formatExpectedReturn(loan.expected_return_date)}</p>
                                        <p><strong>Requested:</strong> {formatDate(loan.CreatedAt)}</p>
                                    </div>
                                    <div class="loan-actions">
                                        <button 
                                            class="approve-btn" 
                                            on:click={() => approveLoan(loan.ID, 'approve')}
                                            disabled={loading}
                                        >
                                            ✅ Approve
                                        </button>
                                        <button 
                                            class="deny-btn" 
                                            on:click={() => approveLoan(loan.ID, 'deny')}
                                            disabled={loading}
                                        >
                                            ❌ Deny
                                        </button>
                                    </div>
                                </div>
                            {/each}
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Pending Returns View -->
            {#if currentView === 'pending-returns'}
                <div class="loans-content">
                    <h2>🔄 Pending Return Requests</h2>
                    {#if loading}
                        <p>Loading pending returns...</p>
                    {:else if pendingReturns.length === 0}
                        <p class="no-items">No pending return requests.</p>
                    {:else}
                        <div class="loans-grid">
                            {#each pendingReturns as loan}
                                <div class="loan-card return-pending">
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
                                            <span class="status-badge return-pending">Return Pending</span>
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
                                        <p><strong>Return Requested:</strong> {formatDate(loan.return_requested_at)}</p>
                                        {#if isOverdue(loan.expected_return_date)}
                                            <p class="overdue-text"><strong>Status:</strong> OVERDUE</p>
                                        {/if}
                                    </div>
                                    <div class="loan-actions">
                                        <button 
                                            class="approve-btn" 
                                            on:click={() => approveReturn(loan.ID, 'approved')}
                                            disabled={loading}
                                        >
                                            ✅ Approve Return
                                        </button>
                                        <button 
                                            class="not-found-btn" 
                                            on:click={() => approveReturn(loan.ID, 'not_found')}
                                            disabled={loading}
                                        >
                                            ❌ Mark as Not Found
                                        </button>
                                    </div>
                                </div>
                            {/each}
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Lost/Missing Items View -->
            {#if currentView === 'lost-missing'}
                <div class="loans-content">
                    <h2>🔍 Lost/Missing Items</h2>
                    <p class="subtitle-text">Items marked as not found or with pending return requests</p>
                    {#if loading}
                        <p>Loading lost/missing items...</p>
                    {:else if lostMissingItems.length === 0}
                        <p class="no-items">No lost or missing items found.</p>
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
                                            {#if loan.status === 'not_found'}
                                                <span class="status-badge not-found">Not Found</span>
                                            {:else if loan.return_requested}
                                                <span class="status-badge return-pending">Return Pending</span>
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
                                        <p><strong>Lab:</strong> {loan.lab_location}</p>
                                        <p><strong>Quantity:</strong> {loan.quantity_borrowed}</p>
                                        <p><strong>Expected Return:</strong> {formatExpectedReturn(loan.expected_return_date)}</p>
                                        {#if loan.status === 'not_found'}
                                            <p><strong>Status:</strong> Marked as not found</p>
                                        {:else}
                                            <p><strong>Status:</strong> Return requested, awaiting approval</p>
                                        {/if}
                                        <p><strong>Borrowed:</strong> {formatDate(loan.CreatedAt)}</p>
                                    </div>
                                    {#if loan.return_requested && loan.return_approval_status === 'pending'}
                                        <div class="return-section">
                                            <p class="return-notice">🔄 Return requested - waiting for approval</p>
                                            <div class="return-actions">
                                                <button 
                                                    class="approve-btn" 
                                                    on:click={() => approveReturn(loan.ID, 'approved')}
                                                    disabled={loading}
                                                >
                                                    ✅ Approve Return
                                                </button>
                                                <button 
                                                    class="not-found-btn" 
                                                    on:click={() => approveReturn(loan.ID, 'not_found')}
                                                    disabled={loading}
                                                >
                                                    ❌ Mark as Not Found
                                                </button>
                                            </div>
                                        </div>
                                    {/if}
                                    {#if loan.status === 'not_found'}
                                        <div class="found-section">
                                            <p class="found-notice">🔍 Item marked as not found</p>
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

            <!-- Item History View -->
            {#if currentView === 'history'}
                <div class="loans-content">
                    <h2>📚 Complete Item History</h2>
                    <p class="subtitle-text">Chronological history of all items - borrowed, returned, lost, and found</p>
                    
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
                                                    {#if item.status === 'approved'}
                                                        📋 Borrowed
                                                    {:else if item.status === 'returned'}
                                                        ✅ Returned
                                                    {:else if item.status === 'not_found'}
                                                        ❌ Lost/Missing
                                                    {:else if item.status === 'denied'}
                                                        ❌ Denied
                                                    {:else if item.status === 'pending'}
                                                        ⏳ Pending
                                                    {:else}
                                                        {item.status.charAt(0).toUpperCase() + item.status.slice(1)}
                                                    {/if}
                                                </span>
                                                {#if item.status === 'approved' && isOverdue(item.expected_return_date) && item.status !== 'returned'}
                                                    <span class="overdue-badge">OVERDUE</span>
                                                {/if}
                                            </div>
                                            <div class="history-date">
                                                <span class="date-label">
                                                    {#if item.status === 'returned'}
                                                        Returned:
                                                    {:else if item.status === 'not_found'}
                                                        Marked Lost:
                                                    {:else if item.status === 'denied'}
                                                        Denied:
                                                    {:else if item.approved_at}
                                                        Approved:
                                                    {:else}
                                                        Requested:
                                                    {/if}
                                                </span>
                                                <span class="date-value">
                                                    {#if item.status === 'returned' && item.return_approved_at}
                                                        {formatDate(item.return_approved_at)}
                                                    {:else if item.approved_at}
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
                                                        <p><strong>Approved by:</strong> {item.approved_by}</p>
                                                    {/if}
                                                </div>
                                                <div class="detail-group">
                                                    <p><strong>Requested:</strong> {formatDate(item.CreatedAt)}</p>
                                                    {#if item.expected_return_date}
                                                        <p><strong>Expected Return:</strong> {formatExpectedReturn(item.expected_return_date)}</p>
                                                    {/if}
                                                    {#if item.return_requested_at}
                                                        <p><strong>Return Requested:</strong> {formatDate(item.return_requested_at)}</p>
                                                    {/if}
                                                    {#if item.status === 'returned' && item.return_approved_at}
                                                        <p><strong>Return Date:</strong> {formatDate(item.return_approved_at)}</p>
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
                            class:active={selectedFilter === 'pending'}
                            on:click={() => filterLabLoans('pending')}
                        >
                            Pending
                        </button>
                        <button 
                            class="filter-btn" 
                            class:active={selectedFilter === 'rejected'}
                            on:click={() => filterLabLoans('rejected')}
                        >
                            Rejected
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
                            Not Found
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
                                    class:overdue={isOverdue(loan.expected_return_date) && loan.approval_status === 'approved' && loan.status !== 'returned'}
                                    class:rejected={loan.approval_status === 'denied'}
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
                                            <span class="status-badge {loan.approval_status}">
                                                {loan.approval_status.charAt(0).toUpperCase() + loan.approval_status.slice(1)}
                                            </span>
                                            {#if isOverdue(loan.expected_return_date) && loan.approval_status === 'approved' && loan.status !== 'returned'}
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
                                            <p><strong>Approved by:</strong> {loan.approved_by}</p>
                                        {/if}
                                    </div>
                                    {#if loan.approval_status === 'approved'}
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
                                        </div>
                                    {/if}
                                    {#if loan.return_requested && loan.return_approval_status === 'pending'}
                                        <div class="return-section">
                                            <p class="return-notice">🔄 Return requested - waiting for approval</p>
                                            <div class="return-actions">
                                                <button 
                                                    class="approve-btn" 
                                                    on:click={() => approveReturn(loan.ID, 'approved')}
                                                    disabled={loading}
                                                >
                                                    ✅ Approve Return
                                                </button>
                                                <button 
                                                    class="not-found-btn" 
                                                    on:click={() => approveReturn(loan.ID, 'not_found')}
                                                    disabled={loading}
                                                >
                                                    ❌ Mark as Not Found
                                                </button>
                                            </div>
                                        </div>
                                    {:else if loan.return_requested && loan.return_approval_status === 'approved'}
                                        <div class="return-section">
                                            <p class="return-status approved">✅ Return approved - Item returned</p>
                                            {#if loan.return_approved_at}
                                                <p class="return-date"><strong>Return Date:</strong> {formatDate(loan.return_approved_at)}</p>
                                            {/if}
                                            {#if isOverdue(loan.expected_return_date)}
                                                <p class="overdue-returned"><strong>Status:</strong> <span class="overdue-mark">⚠️ Returned Overdue</span></p>
                                            {/if}
                                        </div>
                                    {:else if loan.return_requested && loan.return_approval_status === 'not_found'}
                                        <div class="return-section">
                                            <p class="return-status not-found">❌ Item marked as not found</p>
                                        </div>
                                    {/if}
                                    {#if loan.approval_status === 'pending'}
                                        <div class="loan-actions">
                                            <button 
                                                class="approve-btn" 
                                                on:click={() => approveLoan(loan.ID, 'approve')}
                                                disabled={loading}
                                            >
                                                ✅ Approve
                                            </button>
                                            <button 
                                                class="deny-btn" 
                                                on:click={() => approveLoan(loan.ID, 'deny')}
                                                disabled={loading}
                                            >
                                                ❌ Deny
                                            </button>
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
                                            {#if admin.username !== adminInfo.username && admin.username !== 'Srinath'}
                                                <button 
                                                    class="delete-admin-btn" 
                                                    on:click={() => deleteAdmin(admin.id, admin.name, admin.username)}
                                                    title="Delete this admin account"
                                                >
                                                    🗑️ Delete
                                                </button>
                                            {:else if admin.username === adminInfo.username}
                                                <span class="current-user-badge">You</span>
                                            {:else if admin.username === 'Srinath'}
                                                <span class="protected-user-badge">Protected</span>
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
        background: #1e1e2e;
        color: #cdd6f4;
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
        background: #181825;
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
        color: #a6e3a1;
    }

    .message.error {
        background-color: rgba(243, 139, 168, 0.15);
        border: 1px solid rgba(243, 139, 168, 0.4);
        color: #f38ba8;
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
        background: #f38ba8;
        color: #1e1e2e;
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
        background: #eba0ac;
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
        color: #cdd6f4;
        font-size: clamp(1.6rem, 4vw, 2.2rem);
        font-weight: 600;
    }

    .login-form {
        background: #11111b;
        padding: clamp(30px, 6vw, 50px);
        border-radius: 12px;
        box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
        border: 1px solid #313244;
    }

    .form-group {
        margin-bottom: 20px;
        text-align: left;
    }

    .form-group label {
        display: block;
        margin-bottom: 8px;
        font-weight: 600;
        color: #cdd6f4;
        font-size: clamp(0.9rem, 2vw, 1rem);
    }

    .form-group input {
        width: 100%;
        padding: clamp(10px, 2.5vw, 14px);
        border: 2px solid #313244;
        border-radius: 8px;
        font-size: clamp(0.9rem, 2vw, 1rem);
        box-sizing: border-box;
        background: #1e1e2e;
        color: #cdd6f4;
        transition: all 0.3s ease;
    }

    .form-group input:focus {
        border-color: #f2cdcd;
        outline: none;
        box-shadow: 0 0 0 3px rgba(242, 205, 205, 0.25);
    }

    .login-btn {
        width: 100%;
        background: linear-gradient(135deg, #f2cdcd, #eba0ac);
        color: #11111b;
        border: none;
        padding: clamp(12px, 3vw, 18px);
        border-radius: 8px;
        font-size: clamp(1rem, 2.5vw, 1.2rem);
        cursor: pointer;
        font-weight: 600;
        transition: all 0.3s ease;
    }

    .login-btn:hover {
        background: linear-gradient(135deg, #eba0ac, #f5c2e7);
        transform: translateY(-1px);
        box-shadow: 0 8px 25px rgba(242, 205, 205, 0.3);
    }

    .login-btn:disabled {
        background: #313244;
        cursor: not-allowed;
        transform: none;
        box-shadow: none;
        color: #a6adc8;
    }

    /* Dashboard styles */
    .dashboard-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: clamp(20px, 4vw, 40px);
        padding-bottom: 20px;
        border-bottom: 2px solid #313244;
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
        color: #cdd6f4;
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
        color: #cdd6f4;
        margin: 0;
        font-size: clamp(1.8rem, 4vw, 2.5rem);
        font-weight: 600;
    }

    .logout-btn {
        background: linear-gradient(135deg, #f38ba8, #eba0ac);
        color: #11111b;
        border: none;
        padding: clamp(8px, 2vw, 12px) clamp(12px, 3vw, 20px);
        border-radius: 8px;
        cursor: pointer;
        font-size: clamp(0.85rem, 1.8vw, 1rem);
        font-weight: 500;
        transition: all 0.3s ease;
    }

    .logout-btn:hover {
        background: linear-gradient(135deg, #eba0ac, #f5c2e7);
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(243, 139, 168, 0.3);
    }

    /* Navigation tabs */
    .nav-tabs {
        display: flex;
        gap: clamp(5px, 1vw, 15px);
        margin-bottom: clamp(20px, 4vw, 40px);
        border-bottom: 2px solid #313244;
        overflow-x: auto;
    }

    .tab-btn {
        background: none;
        border: none;
        padding: clamp(12px, 3vw, 18px) clamp(15px, 3vw, 25px);
        cursor: pointer;
        border-bottom: 3px solid transparent;
        font-size: clamp(0.9rem, 2vw, 1.1rem);
        position: relative;
        color: #a6adc8;
        transition: all 0.3s ease;
        white-space: nowrap;
        font-weight: 500;
    }

    .tab-btn.active {
        border-bottom-color: #f2cdcd;
        color: #f2cdcd;
        font-weight: 600;
    }

    .tab-btn:hover {
        background: rgba(242, 205, 205, 0.1);
        color: #cdd6f4;
    }

    .badge {
        background: #f38ba8;
        color: #11111b;
        border-radius: 12px;
        padding: 3px 8px;
        font-size: clamp(10px, 1.5vw, 12px);
        margin-left: 8px;
        font-weight: 600;
    }

    /* Dashboard overview */
    .overview-cards {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
        gap: clamp(15px, 3vw, 25px);
        margin-top: 20px;
    }

    .card {
        background: #11111b;
        padding: clamp(25px, 5vw, 40px);
        border-radius: 12px;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
        text-align: center;
        border: 1px solid #313244;
        transition: all 0.3s ease;
    }

    .card:hover {
        transform: translateY(-3px);
        box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
        border-color: #f2cdcd;
    }

    .stat-number {
        font-size: clamp(2.5rem, 8vw, 4rem);
        font-weight: 700;
        color: #f2cdcd;
        margin: 15px 0;
    }

    .card h3 {
        color: #cdd6f4;
        margin-bottom: 15px;
        font-size: clamp(1.1rem, 2.5vw, 1.3rem);
    }

    .card-btn {
        background: linear-gradient(135deg, #f2cdcd, #eba0ac);
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
        background: linear-gradient(135deg, #eba0ac, #f5c2e7);
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
        background: #11111b;
        border: 2px solid #313244;
        color: #a6adc8;
        padding: clamp(6px, 1.5vw, 10px) clamp(12px, 3vw, 18px);
        border-radius: 20px;
        cursor: pointer;
        font-size: clamp(0.8rem, 1.8vw, 0.95rem);
        transition: all 0.3s ease;
        font-weight: 500;
    }

    .filter-btn:hover {
        background: #313244;
        border-color: #f2cdcd;
        color: #cdd6f4;
    }

    .filter-btn.active {
        background: #f2cdcd;
        border-color: #f2cdcd;
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
        background: #11111b;
        border-radius: 12px;
        padding: clamp(18px, 4vw, 25px);
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
        border-left: 5px solid #f2cdcd;
        border: 1px solid #313244;
        transition: all 0.3s ease;
    }

    .loan-card:hover {
        transform: translateY(-2px);
        box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
        border-color: #f2cdcd;
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
        border-left-color: #6c7086;
        background: rgba(108, 112, 134, 0.05);
        opacity: 0.9;
    }

    /* History View Styles */
    .history-search-container {
        background: #11111b;
        padding: 20px;
        border-radius: 12px;
        margin-bottom: 20px;
        border: 1px solid #313244;
        box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
    }

    .history-search-container label {
        display: block;
        margin-bottom: 10px;
        font-weight: 600;
        color: #cdd6f4;
        font-size: 1rem;
    }

    .history-search-input {
        width: 100%;
        padding: 12px 16px;
        border: 2px solid #313244;
        border-radius: 8px;
        font-size: 1rem;
        box-sizing: border-box;
        background: #1e1e2e;
        color: #cdd6f4;
        transition: all 0.3s ease;
    }

    .history-search-input:focus {
        border-color: #f2cdcd;
        outline: none;
        box-shadow: 0 0 0 3px rgba(242, 205, 205, 0.25);
    }

    .search-results-info {
        margin-top: 10px;
        color: #a6adc8;
        font-size: 0.9rem;
        font-style: italic;
    }

    .no-search-results {
        text-align: center;
        padding: 40px 20px;
        background: #11111b;
        border-radius: 12px;
        border: 1px solid #313244;
        margin-top: 20px;
    }

    .no-search-results p {
        color: #cdd6f4;
        font-size: 1.1rem;
        margin-bottom: 15px;
    }

    .clear-search-btn-large {
        background: linear-gradient(135deg, #74c7ec, #89b4fa);
        color: #11111b;
        border: none;
        padding: 10px 20px;
        border-radius: 8px;
        font-size: 1rem;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.3s ease;
    }

    .clear-search-btn-large:hover {
        background: linear-gradient(135deg, #89b4fa, #b4befe);
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
        background: #11111b;
        border-radius: 12px;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
        border: 1px solid #313244;
        transition: all 0.3s ease;
        position: relative;
    }

    .history-item:hover {
        transform: translateY(-2px);
        box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
        border-color: #f2cdcd;
    }

    .history-item.approved {
        border-left: 5px solid #a6e3a1;
    }

    .history-item.returned {
        border-left: 5px solid #94e2d5;
    }

    .history-item.not_found {
        border-left: 5px solid #f38ba8;
    }

    .history-item.denied {
        border-left: 5px solid #6c7086;
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
        border: 3px solid #313244;
        z-index: 2;
        margin-top: 5px;
    }

    .timeline-dot.approved {
        background: #a6e3a1;
        border-color: #a6e3a1;
    }

    .timeline-dot.returned {
        background: #94e2d5;
        border-color: #94e2d5;
    }

    .timeline-dot.not_found {
        background: #f38ba8;
        border-color: #f38ba8;
    }

    .timeline-dot.denied {
        background: #6c7086;
        border-color: #6c7086;
    }

    .timeline-dot.pending {
        background: #f7b955;
        border-color: #f7b955;
        animation: pulse 2s infinite;
    }

    .timeline-line {
        width: 2px;
        flex: 1;
        background: #313244;
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
        color: #cdd6f4;
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
        color: #a6adc8;
        font-weight: 500;
    }

    .date-value {
        font-size: 0.9rem;
        color: #cdd6f4;
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
        color: #a6adc8;
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
    }

    .detail-group strong {
        color: #cdd6f4;
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
        color: #cdd6f4;
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
        color: #6c7086;
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
        color: #a6adc8;
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
    }

    .loan-details strong {
        color: #cdd6f4;
        font-weight: 600;
    }

    .clickable-phone {
        color: #89b4fa;
        cursor: pointer;
        text-decoration: underline;
        text-decoration-style: dotted;
        transition: all 0.2s ease;
        padding: 2px 4px;
        border-radius: 3px;
    }

    .clickable-phone:hover {
        color: #b4befe;
        background-color: #313244;
        text-decoration-style: solid;
    }

    .clickable-phone:focus {
        outline: 2px solid #f2cdcd;
        outline-offset: 2px;
        background-color: #313244;
    }

    .photo-section {
        margin: 15px 0;
        padding: 12px;
        background: #161b22;
        border-radius: 8px;
        border: 1px solid #313244;
    }

    .item-image {
        width: clamp(70px, 15vw, 90px);
        height: clamp(70px, 15vw, 90px);
        border-radius: 8px;
        overflow: hidden;
        border: 2px solid #313244;
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
        color: #a6adc8;
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
        background: linear-gradient(135deg, #f2cdcd, #eba0ac);
        color: white;
    }

    .extend-btn:hover {
        background: linear-gradient(135deg, #eba0ac, #f5c2e7);
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
        border: 2px solid #313244;
        border-radius: 6px;
        text-align: center;
        background: #1e1e2e;
        color: #cdd6f4;
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
    }

    .extend-inputs input:focus {
        border-color: #f2cdcd;
        outline: none;
    }

    .extend-inputs span {
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
        color: #a6adc8;
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
        color: #a6e3a1;
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
        background: #a6e3a1;
        color: #1e1e2e;
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
        color: #94e2d5;
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
        background: #11111b;
        border-radius: 12px;
        border: 1px solid #313244;
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
        color: #a6adc8;
        font-style: italic;
        margin-bottom: 20px;
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
    }

    .credits-footer {
        background: #11111b;
        padding: clamp(15px, 3vw, 20px);
        text-align: center;
        border-top: 1px solid #313244;
        margin-top: 40px;
        color: #a6adc8;
        font-size: clamp(12px, 2vw, 14px);
    }

    .credits-footer a {
        color: #f2cdcd;
        text-decoration: none;
    }

    .credits-footer a:hover {
        text-decoration: underline;
        color: #79c0ff;
    }

    .loan-actions input[type="date"] {
        padding: clamp(6px, 1.5vw, 10px);
        border: 2px solid #313244;
        border-radius: 6px;
        flex: 1;
        background: #1e1e2e;
        color: #cdd6f4;
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
    }

    .loan-actions input[type="date"]:focus {
        border-color: #f2cdcd;
        outline: none;
    }

    .no-items {
        text-align: center;
        color: #a6adc8;
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
        background: #313244;
        border-radius: 12px;
        padding: 24px;
        border: 1px solid #45475a;
        box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
    }

    .form-group {
        margin-bottom: 20px;
    }

    .form-group label {
        display: block;
        margin-bottom: 8px;
        font-weight: 500;
        color: #cdd6f4;
    }

    .form-group input[type="text"],
    .form-group input[type="password"] {
        width: 100%;
        padding: 12px 16px;
        background: #1e1e2e;
        border: 2px solid #45475a;
        border-radius: 8px;
        color: #cdd6f4;
        font-size: 1rem;
        transition: all 0.2s ease;
        box-sizing: border-box;
    }

    .form-group input[type="text"]:focus,
    .form-group input[type="password"]:focus {
        outline: none;
        border-color: #89b4fa;
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
        background: linear-gradient(135deg, #89b4fa, #74c7ec);
        color: #1e1e2e;
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
        background: #6c7086;
        color: #cdd6f4;
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
        background: #313244;
        border-radius: 12px;
        padding: 24px;
        margin-bottom: 24px;
        border: 1px solid #45475a;
    }

    .section-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: 20px;
    }

    .section-header h3 {
        margin: 0;
        color: #cdd6f4;
        font-size: 1.2rem;
    }

    .toggle-btn {
        background: #89b4fa;
        color: #1e1e2e;
        border: none;
        padding: 8px 16px;
        border-radius: 6px;
        font-weight: 500;
        cursor: pointer;
        transition: all 0.2s ease;
    }

    .toggle-btn:hover {
        background: #74c7ec;
        transform: translateY(-1px);
    }

    .refresh-btn {
        background: #a6e3a1;
        color: #1e1e2e;
        border: none;
        padding: 8px 16px;
        border-radius: 6px;
        font-weight: 500;
        cursor: pointer;
        transition: all 0.2s ease;
    }

    .refresh-btn:hover {
        background: #94e2d5;
        transform: translateY(-1px);
    }

    .admin-list {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
        gap: 16px;
    }

    .admin-card {
        background: #1e1e2e;
        border-radius: 8px;
        padding: 20px;
        border: 1px solid #45475a;
        transition: all 0.2s ease;
        display: flex;
        flex-direction: column;
        justify-content: space-between;
    }

    .admin-card:hover {
        border-color: #89b4fa;
        box-shadow: 0 4px 12px rgba(137, 180, 250, 0.1);
    }

    .admin-info h4 {
        margin: 0 0 8px 0;
        color: #cdd6f4;
        font-size: 1.1rem;
    }

    .admin-username {
        margin: 4px 0;
        color: #89b4fa;
        font-weight: 500;
    }

    .admin-role {
        margin: 8px 0;
        font-weight: 600;
    }

    .admin-date {
        margin: 8px 0 0 0;
        color: #6c7086;
        font-size: 0.9rem;
    }

    .admin-actions {
        display: flex;
        justify-content: flex-end;
        align-items: center;
        margin-top: 16px;
        padding-top: 12px;
        border-top: 1px solid #45475a;
    }

    .delete-admin-btn {
        background: #f38ba8;
        color: #1e1e2e;
        border: none;
        padding: 6px 12px;
        border-radius: 6px;
        font-weight: 500;
        cursor: pointer;
        transition: all 0.2s ease;
        font-size: 0.9rem;
    }

    .delete-admin-btn:hover {
        background: #eba0ac;
        transform: translateY(-1px);
    }

    .current-user-badge {
        background: #a6e3a1;
        color: #1e1e2e;
        padding: 4px 8px;
        border-radius: 4px;
        font-size: 0.8rem;
        font-weight: 600;
    }

    .protected-user-badge {
        background: #cba6f7;
        color: #1e1e2e;
        padding: 4px 8px;
        border-radius: 4px;
        font-size: 0.8rem;
        font-weight: 600;
    }

    .danger-zone {
        border-color: #f38ba8 !important;
        background: rgba(243, 139, 168, 0.1);
    }

    .danger-zone .section-header h3 {
        color: #f38ba8;
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
        background: #1e1e2e;
        border-radius: 8px;
        border: 1px solid #45475a;
    }

    .danger-info h4 {
        margin: 0 0 4px 0;
        color: #f38ba8;
    }

    .danger-info p {
        margin: 0;
        color: #cdd6f4;
        font-size: 0.9rem;
    }

    .danger-btn {
        background: #f38ba8;
        color: #1e1e2e;
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
        background: #eba0ac;
        transform: translateY(-1px);
    }

    .link-btn {
        background: none;
        border: none;
        color: #89b4fa;
        cursor: pointer;
        text-decoration: underline;
        font-size: inherit;
    }

    .link-btn:hover {
        color: #74c7ec;
    }

    .no-data {
        text-align: center;
        color: #6c7086;
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