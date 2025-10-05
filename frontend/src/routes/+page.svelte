<script>
    import { onMount } from 'svelte';

    // State management
    let currentView = 'home'; // 'home', 'borrow', 'return'
    let items = [];
    let loans = [];
    let loading = false;
    let isSubmitting = false;
    let message = '';
    let messageType = ''; // 'success' or 'error'

    // Borrow form data
    let borrowForm = {
        borrower_name: '',
        borrower_phone: '',
        item_name: '',
        lab_location: '',
        quantity_borrowed: 1,
        return_days: 1,
        return_hours: 0,
        purpose: '',
        item_photo: null
    };

    // Lab locations
    const labLocations = [
        { value: 'Main Lab', label: 'Main Lab' },
        { value: 'Mech Lab', label: 'Mech Lab' },
        { value: 'Control Lab', label: 'Control Lab' }
    ];

    // Return search
    let searchQuery = '';
    let filteredLoans = [];

    // Load items for borrowing
    async function loadItems() {
        try {
            const response = await fetch('/api/items');
            if (response.ok) {
                items = await response.json();
            }
        } catch (e) {
            console.error('Failed to load items:', e);
        }
    }

    // Load active loans for returning
    async function loadActiveLoans() {
        loading = true;
        try {
            const response = await fetch('/api/loans/active');
            if (response.ok) {
                loans = await response.json();
                filteredLoans = loans;
            }
        } catch (e) {
            showMessage('Failed to load loans', 'error');
        } finally {
            loading = false;
        }
    }

    // Submit borrow request with file upload
	async function submitBorrow() {
		isSubmitting = true;
		
		try {
			const formData = new FormData();
			formData.append('borrower_name', borrowForm.borrower_name);
			formData.append('borrower_phone', borrowForm.borrower_phone);
			formData.append('item_name', borrowForm.item_name);
			formData.append('lab_location', borrowForm.lab_location);
			formData.append('quantity_borrowed', borrowForm.quantity_borrowed.toString());
			
			// Calculate expected return date
			const returnDate = new Date();
			returnDate.setDate(returnDate.getDate() + borrowForm.return_days);
			returnDate.setHours(returnDate.getHours() + borrowForm.return_hours);
			const expectedReturnDate = returnDate.toISOString().split('T')[0];
			formData.append('expected_return_date', expectedReturnDate);
			
			formData.append('purpose', borrowForm.purpose);
			
			// Add photo if selected
			if (borrowForm.item_photo) {
				formData.append('item_photo', borrowForm.item_photo);
			}

			const response = await fetch('/api/borrow', {
				method: 'POST',
				body: formData
			});

			const result = await response.json();
			
			if (response.ok) {
				showMessage(result.message, 'success');
				resetBorrowForm();
			} else {
				showMessage(result.error || 'Failed to submit borrow request', 'error');
			}
		} catch (error) {
			console.error('Error submitting borrow request:', error);
			showMessage('Network error. Please try again.', 'error');
		} finally {
			isSubmitting = false;
		}
	}

    // Return an item
    async function returnItem(loanId) {
        loading = true;
        try {
            const response = await fetch(`/api/return/${loanId}`, {
                method: 'POST'
            });

            if (response.ok) {
                const result = await response.json();
                showMessage(result.message, 'success');
                loadActiveLoans(); // Refresh the list
            } else {
                const error = await response.json();
                showMessage(error.error || 'Failed to return item', 'error');
            }
        } catch (e) {
            showMessage('Failed to return item', 'error');
        } finally {
            loading = false;
        }
    }

    // Search loans
    function searchLoans() {
        if (!searchQuery.trim()) {
            filteredLoans = loans;
        } else {
            filteredLoans = loans.filter(loan => 
                loan.borrower_name.toLowerCase().includes(searchQuery.toLowerCase()) ||
                loan.item_name.toLowerCase().includes(searchQuery.toLowerCase())
            );
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

    function handlePhotoUpload(event) {
        const file = event.target.files[0];
        if (file) {
            // Validate file size (max 10MB)
            const maxSize = 10 * 1024 * 1024; // 10MB
            if (file.size > maxSize) {
                showMessage('File size too large. Maximum allowed size is 10MB.', 'error');
                event.target.value = ''; // Clear the input
                return;
            }

            // Validate file type
            const allowedTypes = [
                'image/jpeg', 'image/jpg', 'image/png', 'image/webp', 
                'image/heic', 'image/heif', 'image/gif', 'image/bmp'
            ];
            
            if (!allowedTypes.includes(file.type)) {
                showMessage('Unsupported file format. Please use JPG, PNG, WEBP, HEIC, or other common image formats.', 'error');
                event.target.value = ''; // Clear the input
                return;
            }

            borrowForm.item_photo = file;
            showMessage(`Photo selected: ${file.name} (${(file.size / 1024 / 1024).toFixed(2)}MB)`, 'success');
        }
    }

    function resetBorrowForm() {
        borrowForm = {
            borrower_name: '',
            borrower_phone: '',
            item_name: '',
            lab_location: '',
            quantity_borrowed: 1,
            return_days: 1,
            return_hours: 0,
            purpose: '',
            item_photo: null
        };
        
        // Clear the file input
        const photoInput = document.getElementById('photo');
        if (photoInput) {
            photoInput.value = '';
        }
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

    function goHome() {
        currentView = 'home';
        message = '';
    }

    function goToBorrow() {
        currentView = 'borrow';
        // No need to load items since users can enter any item name
    }

    function goToReturn() {
        currentView = 'return';
        loadActiveLoans();
    }

    // Reactive statement for search
    $: if (searchQuery !== undefined) {
        searchLoans();
    }
</script>

<div class="container">
    <main class="main-content">
        <div class="header">
            <div class="header-main">
                <div class="logo-title">
                    <button class="logo-button" on:click={() => currentView = 'home'} aria-label="Go to home page">
                        <img src="/rrc_logo.png" alt="RRC Logo" class="logo clickable-logo" />
                    </button>
                    <div class="title-section">
                        <h1>Robotics Research Centre</h1>
                        <p class="subtitle">Lab Equipment Management System</p>
                    </div>
                </div>
                <a href="/admin" class="admin-link">Admin Login</a>
            </div>
        </div>

    <!-- Message Display -->
    {#if message}
        <div class="message {messageType}">
            {message}
        </div>
    {/if}

    <!-- Home View - Two Main Options -->
    {#if currentView === 'home'}
        <div class="home-options">
            <h2>What would you like to do?</h2>
            <div class="option-buttons">
                <button class="option-btn borrow-btn" on:click={goToBorrow}>
                    📦 Borrow Item
                    <p>Take equipment from the lab</p>
                </button>
                <button class="option-btn return-btn" on:click={goToReturn}>
                    ↩️ Return Item
                    <p>Return borrowed equipment</p>
                </button>
            </div>
        </div>
    {/if}

    <!-- Borrow View -->
    {#if currentView === 'borrow'}
        <div class="form-container">
            <h2>📦 Borrow Equipment</h2>
            <button class="back-btn" on:click={goHome}>← Back to Home</button>
            
            <form on:submit|preventDefault={submitBorrow}>
                <div class="form-group">
                    <label for="name">Your Name *</label>
                    <input 
                        type="text" 
                        id="name" 
                        bind:value={borrowForm.borrower_name} 
                        required
                        placeholder="Enter your full name"
                    />
                </div>

                <div class="form-group">
                    <label for="phone">Phone Number *</label>
                    <input 
                        type="tel" 
                        id="phone" 
                        bind:value={borrowForm.borrower_phone} 
                        required
                        placeholder="Enter your phone number"
                    />
                </div>

                <div class="form-group">
                    <label for="item">Item Name *</label>
                    <input 
                        type="text" 
                        id="item" 
                        bind:value={borrowForm.item_name} 
                        required
                        placeholder="Enter the item you want to borrow (e.g., Multimeter, Arduino board, etc.)"
                    />
                    <small class="help-text">Enter any equipment available in the lab</small>
                </div>

                <div class="form-group">
                    <label for="lab">Lab Location *</label>
                    <select 
                        id="lab" 
                        bind:value={borrowForm.lab_location} 
                        required
                    >
                        <option value="">Select the lab where the item is located</option>
                        {#each labLocations as lab}
                            <option value={lab.value}>{lab.label}</option>
                        {/each}
                    </select>
                </div>

                <div class="form-group">
                    <label for="quantity">Quantity *</label>
                    <input 
                        type="number" 
                        id="quantity" 
                        bind:value={borrowForm.quantity_borrowed} 
                        min="1" 
                        required
                    />
                </div>

                <div class="form-group">
                    <label>Return Time *</label>
                    <div class="time-inputs">
                        <div class="time-input">
                            <label for="days">Days:</label>
                            <input 
                                type="number" 
                                id="days" 
                                bind:value={borrowForm.return_days} 
                                min="0" 
                                max="30"
                                required
                            />
                        </div>
                        <div class="time-input">
                            <label for="hours">Hours:</label>
                            <input 
                                type="number" 
                                id="hours" 
                                bind:value={borrowForm.return_hours} 
                                min="0" 
                                max="23"
                            />
                        </div>
                    </div>
                    <small class="help-text">
                        Expected return: {borrowForm.return_days} day{borrowForm.return_days !== 1 ? 's' : ''} 
                        {#if borrowForm.return_hours > 0}and {borrowForm.return_hours} hour{borrowForm.return_hours !== 1 ? 's' : ''}{/if}
                        from now
                    </small>
                </div>

                <div class="form-group">
                    <label for="purpose">Purpose *</label>
                    <textarea 
                        id="purpose" 
                        bind:value={borrowForm.purpose} 
                        required
                        placeholder="Why do you need this item? (e.g., for project work, assignment, research, etc.)"
                        rows="3"
                    ></textarea>
                </div>

                <div class="form-group">
                    <label for="photo">Item Photo *</label>
                    <input 
                        type="file" 
                        id="photo" 
                        accept="image/jpeg,image/jpg,image/png,image/webp,image/heic,image/heif,image/gif,image/bmp"
                        capture="environment"
                        required
                        on:change={handlePhotoUpload}
                    />
                    <small class="help-text">Take a photo or upload an image (JPG, PNG, WEBP, HEIC supported - max 10MB)</small>
                    {#if borrowForm.item_photo}
                        <div class="photo-preview">
                            <p>✅ Photo selected: {borrowForm.item_photo.name}</p>
                        </div>
                    {/if}
                </div>

                <button type="submit" class="submit-btn" disabled={isSubmitting}>
                    {isSubmitting ? 'Processing...' : 'Borrow Item'}
                </button>
            </form>
        </div>
    {/if}

    <!-- Return View -->
    {#if currentView === 'return'}
        <div class="return-container">
            <h2>↩️ Return Equipment</h2>
            <button class="back-btn" on:click={goHome}>← Back to Home</button>
            
            <div class="search-container">
                <label for="search">Search by your name or item name:</label>
                <input 
                    type="text" 
                    id="search" 
                    bind:value={searchQuery} 
                    placeholder="Search loans..."
                />
            </div>

            {#if loading}
                <p>Loading borrowed items...</p>
            {:else if filteredLoans.length === 0}
                <p class="no-loans">No borrowed items found.</p>
            {:else}
                <div class="loans-list">
                    <h3>Borrowed Items:</h3>
                    {#each filteredLoans as loan}
                        <div class="loan-card" class:missing={loan.status === 'not_found'}>
                            {#if loan.status === 'not_found'}
                                <div class="missing-header">
                                    <span class="missing-badge">⚠️ MISSING/NOT FOUND</span>
                                </div>
                            {/if}
                            <div class="loan-info">
                                <h4>{loan.item_name}</h4>
                                <p><strong>Borrower:</strong> {loan.borrower_name}</p>
                                <p><strong>Phone:</strong> {loan.borrower_phone}</p>
                                <p><strong>Lab:</strong> {loan.lab_location}</p>
                                <p><strong>Quantity:</strong> {loan.quantity_borrowed}</p>
                                <p><strong>Purpose:</strong> {loan.purpose}</p>
                                <p><strong>Borrowed on:</strong> {formatDate(loan.CreatedAt)}</p>
                                <p><strong>Expected return:</strong> {formatExpectedReturn(loan.expected_return_date)}</p>
                                {#if loan.status === 'not_found'}
                                    <p class="missing-note"><strong>Status:</strong> This item has been marked as missing/not found by administrators</p>
                                {:else if loan.status === 'returned'}
                                    <p class="returned-note"><strong>Status:</strong> Recently returned and processed by admin</p>
                                {:else if loan.return_requested}
                                    <p class="return-pending-note"><strong>Status:</strong> Return request submitted - waiting for admin approval</p>
                                {/if}
                            </div>
                            {#if loan.status === 'not_found'}
                                <div class="missing-actions">
                                    <p class="missing-contact">Please contact administration if you have found this item</p>
                                </div>
                            {:else if loan.status === 'returned'}
                                <div class="returned-actions">
                                    <p class="returned-message">🎉 Successfully returned! This item will be removed from your list tomorrow.</p>
                                </div>
                            {:else if loan.return_requested}
                                <div class="return-pending-actions">
                                    <p class="return-pending-message">✅ Return request submitted! Waiting for admin approval.</p>
                                </div>
                            {:else}
                                <button 
                                    class="return-btn-action" 
                                    on:click={() => returnItem(loan.ID)}
                                    disabled={loading}
                                >
                                    ✅ Mark as Returned
                                </button>
                            {/if}
                        </div>
                    {/each}
                </div>
            {/if}
        </div>
    {/if}
    </main>
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
        height: 100vh;
        overflow: hidden;
    }

    :global(html) {
        height: 100vh;
        overflow: hidden;
    }

    .container {
        max-width: 1200px;
        margin: 0 auto;
        padding: clamp(10px, 2vw, 20px);
        font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
        background: #181825;
        height: calc(100vh - 120px);
        overflow-y: auto;
        display: flex;
        flex-direction: column;
    }

    .main-content {
        flex: 1;
        overflow-y: auto;
        padding-bottom: 20px;
    }

    .header {
        margin-bottom: clamp(15px, 3vw, 25px);
    }

    .header-main {
        display: flex;
        justify-content: space-between;
        align-items: center;
        flex-wrap: wrap;
        gap: 15px;
        width: 100%;
    }

    .logo-title {
        display: flex;
        align-items: center;
        gap: clamp(15px, 3vw, 20px);
        flex: 1;
    }

    .logo {
        height: clamp(50px, 8vw, 80px);
        width: auto;
        border-radius: 8px;
        box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
    }

    .logo-button {
        background: none;
        border: none;
        padding: 0;
        cursor: pointer;
        border-radius: 8px;
        transition: transform 0.2s ease, box-shadow 0.2s ease;
    }

    .logo-button:hover {
        transform: scale(1.05);
    }

    .logo-button:hover .logo {
        box-shadow: 0 6px 16px rgba(137, 180, 250, 0.3);
    }

    .clickable-logo {
        transition: box-shadow 0.2s ease;
    }

    .title-section {
        display: flex;
        flex-direction: column;
        gap: 2px;
    }

    h1 {
        color: #cdd6f4;
        margin: 0;
        font-size: clamp(1.6rem, 4.5vw, 2.4rem);
        font-weight: 600;
        line-height: 1.2;
    }

    .subtitle {
        color: #a6adc8;
        font-size: clamp(0.9rem, 2vw, 1.1rem);
        margin: 0;
        font-weight: normal;
        line-height: 1.3;
    }

    .credits-footer {
        background: #11111b;
        padding: clamp(8px, 1.5vw, 12px);
        text-align: center;
        border-top: 1px solid #313244;
        color: #a6adc8;
        font-size: clamp(10px, 1.5vw, 12px);
        position: fixed;
        bottom: 0;
        left: 0;
        right: 0;
        z-index: 100;
        height: 60px;
        display: flex;
        flex-direction: column;
        justify-content: center;
        gap: 2px;
    }

    .credits-footer p {
        margin: 0;
        line-height: 1.2;
    }

    .credits-footer a {
        color: #f2cdcd;
        text-decoration: none;
    }

    .credits-footer a:hover {
        text-decoration: underline;
        color: #f5c2e7;
    }

    .admin-link {
        color: #a6adc8;
        text-decoration: none;
        font-size: clamp(12px, 2vw, 14px);
        padding: clamp(6px, 1.5vw, 12px) clamp(12px, 3vw, 16px);
        border: 1px solid #313244;
        border-radius: 6px;
        transition: all 0.3s ease;
        white-space: nowrap;
    }

    .admin-link:hover {
        background: #11111b;
        color: #cdd6f4;
        border-color: #f2cdcd;
    }

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

    /* Home View Styles */
    .home-options {
        text-align: center;
    }

    .option-buttons {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
        gap: clamp(15px, 3vw, 25px);
        margin-top: clamp(20px, 4vw, 35px);
        max-width: 1000px;
        margin-left: auto;
        margin-right: auto;
    }

    .option-btn {
        padding: clamp(20px, 4vw, 35px) clamp(15px, 3vw, 25px);
        border: none;
        border-radius: 12px;
        font-size: clamp(1.1rem, 2.5vw, 1.4rem);
        cursor: pointer;
        transition: all 0.3s ease;
        text-align: center;
        border: 2px solid transparent;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
    }

    .option-btn:hover {
        transform: translateY(-5px);
        box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
    }

    .borrow-btn {
        background: linear-gradient(135deg, #f2cdcd, #eba0ac);
        color: #11111b;
    }

    .borrow-btn:hover {
        background: linear-gradient(135deg, #eba0ac, #f5c2e7);
        border-color: #f2cdcd;
    }

    .return-btn {
        background: linear-gradient(135deg, #74c7ec, #89b4fa);
        color: #11111b;
    }

    .return-btn:hover {
        background: linear-gradient(135deg, #89b4fa, #b4befe);
        border-color: #74c7ec;
    }

    .option-btn p {
        font-size: clamp(0.9rem, 2vw, 1rem);
        margin-top: 15px;
        opacity: 0.9;
        line-height: 1.4;
    }

    /* Form Styles */
    .form-container, .return-container {
        background: #11111b;
        padding: clamp(15px, 3vw, 25px);
        border-radius: 12px;
        margin-top: 15px;
        border: 1px solid #313244;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
        max-width: 800px;
        margin-left: auto;
        margin-right: auto;
    }

    .back-btn {
        background: #313244;
        color: #cdd6f4;
        border: none;
        padding: clamp(8px, 2vw, 12px) clamp(16px, 3vw, 24px);
        border-radius: 8px;
        cursor: pointer;
        margin-bottom: 20px;
        font-size: clamp(0.85rem, 2vw, 1rem);
        transition: all 0.3s ease;
    }

    .back-btn:hover {
        background: #45475a;
        transform: translateY(-1px);
    }

    .form-group {
        margin-bottom: clamp(16px, 3vw, 24px);
    }

    .form-group label {
        display: block;
        margin-bottom: 8px;
        font-weight: 600;
        color: #cdd6f4;
        font-size: clamp(0.9rem, 2vw, 1rem);
    }

    .form-group input,
    .form-group select,
    .form-group textarea {
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

    .form-group input:focus,
    .form-group select:focus,
    .form-group textarea:focus {
        border-color: #f2cdcd;
        outline: none;
        box-shadow: 0 0 0 3px rgba(242, 205, 205, 0.25);
    }

    .help-text {
        color: #a6adc8;
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
        margin-top: 5px;
        display: block;
    }

    .time-inputs {
        display: grid;
        grid-template-columns: 1fr 1fr;
        gap: clamp(10px, 2vw, 15px);
        margin-bottom: 10px;
    }

    .time-input label {
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
        margin-bottom: 5px;
        font-weight: 500;
    }

    .photo-preview {
        margin-top: 10px;
        padding: 12px;
        background: rgba(166, 227, 161, 0.15);
        border-radius: 8px;
        border: 1px solid rgba(166, 227, 161, 0.3);
    }

    .photo-preview p {
        margin: 0;
        color: #a6e3a1;
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
    }

    .submit-btn {
        background: linear-gradient(135deg, #f2cdcd, #eba0ac);
        color: #11111b;
        border: none;
        padding: clamp(12px, 3vw, 18px) clamp(20px, 4vw, 30px);
        border-radius: 8px;
        font-size: clamp(1rem, 2.5vw, 1.2rem);
        cursor: pointer;
        width: 100%;
        transition: all 0.3s ease;
        font-weight: 600;
        margin-top: 10px;
    }

    .submit-btn:hover {
        background: linear-gradient(135deg, #eba0ac, #f5c2e7);
        transform: translateY(-2px);
        box-shadow: 0 8px 25px rgba(242, 205, 205, 0.3);
    }

    .submit-btn:disabled {
        background: #313244;
        cursor: not-allowed;
        transform: none;
        box-shadow: none;
        color: #a6adc8;
    }

    /* Available Items */
    .available-items {
        margin-top: clamp(20px, 4vw, 40px);
    }

    .items-grid {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
        gap: clamp(15px, 3vw, 20px);
        margin-top: 15px;
    }

    .item-card {
        padding: clamp(15px, 3vw, 20px);
        border: 1px solid #313244;
        border-radius: 8px;
        background: #11111b;
        transition: all 0.3s ease;
    }

    .item-card:hover {
        border-color: #f2cdcd;
        transform: translateY(-2px);
        box-shadow: 0 8px 25px rgba(0, 0, 0, 0.3);
    }

    .item-card.unavailable {
        opacity: 0.6;
        background: #1e1e2e;
    }

    /* Return Styles */
    .search-container {
        margin-bottom: clamp(20px, 4vw, 30px);
    }

    .search-container input {
        width: 100%;
        padding: clamp(12px, 3vw, 16px);
        border: 2px solid #313244;
        border-radius: 8px;
        font-size: clamp(0.9rem, 2vw, 1rem);
        margin-top: 5px;
        background: #1e1e2e;
        color: #cdd6f4;
        transition: all 0.3s ease;
        box-sizing: border-box;
    }

    .search-container input:focus {
        border-color: #f2cdcd;
        outline: none;
        box-shadow: 0 0 0 3px rgba(242, 205, 205, 0.25);
    }

    .loans-list {
        margin-top: 20px;
    }

    .loan-card {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: clamp(15px, 3vw, 25px);
        border: 1px solid #313244;
        border-radius: 8px;
        margin-bottom: 15px;
        background: #11111b;
        transition: all 0.3s ease;
        flex-wrap: wrap;
        gap: 15px;
    }

    .loan-card:hover {
        border-color: #f2cdcd;
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(0, 0, 0, 0.3);
    }

    .loan-info {
        flex: 1;
        min-width: 250px;
    }

    .loan-info h4 {
        margin: 0 0 10px 0;
        color: #cdd6f4;
        font-size: clamp(1.1rem, 2.5vw, 1.3rem);
    }

    .loan-info p {
        margin: clamp(3px, 1vw, 6px) 0;
        color: #a6adc8;
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
    }

    .loan-info strong {
        color: #cdd6f4;
    }

    .return-btn-action {
        background: linear-gradient(135deg, #f38ba8, #eba0ac);
        color: #11111b;
        border: none;
        padding: clamp(10px, 2vw, 14px) clamp(16px, 3vw, 24px);
        border-radius: 8px;
        cursor: pointer;
        white-space: nowrap;
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
        font-weight: 500;
        transition: all 0.3s ease;
        min-width: 140px;
    }

    .return-btn-action:hover {
        background: linear-gradient(135deg, #eba0ac, #f5c2e7);
        transform: translateY(-1px);
        box-shadow: 0 4px 15px rgba(243, 139, 168, 0.3);
    }

    .return-btn-action:disabled {
        background: #313244;
        cursor: not-allowed;
        transform: none;
        box-shadow: none;
        color: #a6adc8;
    }

    .no-loans {
        text-align: center;
        color: #a6adc8;
        font-style: italic;
        padding: clamp(30px, 6vw, 50px);
        font-size: clamp(1rem, 2vw, 1.1rem);
    }

    /* Enhanced Responsive Design */
    @media (max-width: 768px) {
        .container {
            padding: 15px;
        }

        .header {
            flex-direction: column;
            text-align: center;
            gap: 10px;
        }

        .option-buttons {
            grid-template-columns: 1fr;
            gap: 20px;
            margin-top: 30px;
        }

        .option-btn {
            padding: 30px 20px;
        }

        .form-container, .return-container {
            padding: 20px;
            margin: 0 5px 20px 5px;
        }

        .time-inputs {
            grid-template-columns: 1fr;
        }

        .loan-card {
            flex-direction: column;
            align-items: flex-start;
            padding: 20px;
        }

        .loan-info {
            width: 100%;
            margin-bottom: 15px;
        }

        .return-btn-action {
            width: 100%;
            min-width: unset;
        }

        .items-grid {
            grid-template-columns: 1fr;
        }

        .admin-link {
            order: -1;
            margin-bottom: 10px;
        }

        .logo-title {
            flex-direction: column;
            text-align: center;
            gap: 10px;
        }

        .header-main {
            flex-direction: column;
            align-items: center;
            gap: 15px;
        }
    }

    /* Extra small screens (phones in portrait) */
    @media (max-width: 480px) {
        .container {
            padding: 10px;
        }

        .form-container, .return-container {
            margin: 0;
            border-radius: 8px;
            padding: 15px;
        }

        .option-btn {
            padding: 25px 15px;
        }

        h1 {
            font-size: 1.8rem;
        }

        .subtitle {
            font-size: 1rem;
        }
    }

    /* Large screens */
    @media (min-width: 1200px) {
        .option-buttons {
            max-width: 1200px;
        }

        .items-grid {
            grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
        }
    }

    /* Extra large screens */
    @media (min-width: 1600px) {
        .container {
            max-width: 1600px;
        }

        .option-buttons {
            max-width: 1400px;
        }
    }

    /* Missing item styles */
    .loan-card.missing {
        border: 2px solid #f38ba8;
        background: rgba(243, 139, 168, 0.1);
    }

    .missing-header {
        margin-bottom: 15px;
    }

    .missing-badge {
        background: #f38ba8;
        color: #1e1e2e;
        padding: 5px 10px;
        border-radius: 4px;
        font-weight: bold;
        font-size: 0.9rem;
    }

    .missing-note {
        color: #f38ba8;
        font-weight: 500;
        margin-top: 10px;
    }

    .missing-actions {
        padding: 15px;
        background: rgba(243, 139, 168, 0.15);
        border-radius: 6px;
        text-align: center;
    }

    .missing-contact {
        color: #cdd6f4;
        font-style: italic;
        margin: 0;
    }

    .return-pending-note {
        color: #fab387;
        font-weight: 500;
        margin-top: 10px;
    }

    .return-pending-actions {
        padding: 15px;
        background: rgba(166, 227, 161, 0.15);
        border-radius: 6px;
        text-align: center;
    }

    .return-pending-message {
        color: #a6e3a1;
        font-weight: 600;
        margin: 0;
        font-size: 1.1rem;
    }

    .returned-note {
        color: #94e2d5;
        font-weight: 500;
        margin-top: 10px;
    }

    .returned-actions {
        padding: 15px;
        background: rgba(148, 226, 213, 0.15);
        border-radius: 6px;
        text-align: center;
    }

    .returned-message {
        color: #94e2d5;
        font-weight: 600;
        margin: 0;
        font-size: 1.1rem;
    }
</style>