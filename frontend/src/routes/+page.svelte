<script>
    import { onMount } from 'svelte';

    // State management
    let currentView = 'home'; // 'home', 'borrow', 'return', 'printer-guide'
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

    // Quick duration choices - one tap instead of two number inputs
    const durationPresets = [
        { label: '2 hours', days: 0, hours: 2 },
        { label: '1 day', days: 1, hours: 0 },
        { label: '3 days', days: 3, hours: 0 },
        { label: '1 week', days: 7, hours: 0 }
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

    // Simple reactive statement that explicitly depends on both variables
    $: updateFilteredLoans(loans, searchQuery);
    
    function updateFilteredLoans(currentLoans, currentSearchQuery) {
        if (!currentLoans) {
            filteredLoans = [];
            return;
        }
        
        let loansToFilter = [...currentLoans]; // Create a copy
        
        if (currentSearchQuery && currentSearchQuery.trim()) {
            const query = currentSearchQuery.toLowerCase().trim();
            loansToFilter = currentLoans.filter(loan => {
                const borrowerName = (loan.borrower_name || '').toLowerCase();
                const itemName = (loan.item_name || '').toLowerCase();
                const borrowerPhone = (loan.borrower_phone || '').toLowerCase();
                return borrowerName.includes(query) || itemName.includes(query) || borrowerPhone.includes(query);
            });
        }
        
        // Sort loans by priority: Missing -> Borrowed -> Returned
        filteredLoans = loansToFilter.sort((a, b) => {
            // Categorize loans with priority order
            const getLoanPriority = (loan) => {
                if (loan.status === 'not_found') return 1;  // Missing items (highest priority)
                if (loan.status === 'active') return 2;     // Borrowed items
                if (loan.status === 'returned') return 3;   // Returned items (lowest priority)
                return 4;                                   // Other
            };
            
            const priorityA = getLoanPriority(a);
            const priorityB = getLoanPriority(b);
            
            if (priorityA !== priorityB) {
                return priorityA - priorityB;
            }
            
            // Within same priority, sort by date (newest first)
            return new Date(b.CreatedAt) - new Date(a.CreatedAt);
        });
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

    // Name and phone are remembered on this device so repeat borrowers only
    // have to fill in the item.
    const CONTACT_KEY = 'rrc_contact';

    function loadSavedContact() {
        try {
            const saved = JSON.parse(localStorage.getItem(CONTACT_KEY) || 'null');
            if (saved) {
                borrowForm.borrower_name = saved.name || '';
                borrowForm.borrower_phone = saved.phone || '';
            }
        } catch (e) {
            // Ignore unreadable storage and just start with an empty form
        }
    }

    function saveContact() {
        try {
            localStorage.setItem(CONTACT_KEY, JSON.stringify({
                name: borrowForm.borrower_name,
                phone: borrowForm.borrower_phone
            }));
        } catch (e) {
            // Saving is a convenience - never block a borrow on it
        }
    }

    function forgetContact() {
        try {
            localStorage.removeItem(CONTACT_KEY);
        } catch (e) {
            // Nothing to do
        }
        borrowForm.borrower_name = '';
        borrowForm.borrower_phone = '';
        showMessage('Saved details cleared', 'success');
    }

    function setDuration(preset) {
        borrowForm.return_days = preset.days;
        borrowForm.return_hours = preset.hours;
        showCustomDuration = false;
    }

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
				saveContact();
				resetBorrowForm();
				// Redirect to home view first
				currentView = 'home';
				// Then show the success message
				showMessage(result.message || 'Item borrowed successfully!', 'success');
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

    // Helper functions
    function showMessage(text, type) {
        message = text;
        messageType = type;
        setTimeout(() => {
            message = '';
            messageType = '';
        }, 5000);
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
            // Keep the person signed in on this device
            borrower_name: borrowForm.borrower_name,
            borrower_phone: borrowForm.borrower_phone,
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

    function getLabContactInfo(labLocation) {
        const contacts = {
            'Main Lab': { name: 'Tarun', phone: '9677058594' },
            'Control Lab': { name: 'Sarthak', phone: '8849539601' }, 
            'Mech Lab': { name: 'Abhinav', phone: '9602758064' }
        };
        return contacts[labLocation] || { name: 'Administration', phone: null };
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

    function goHome(clearMsg = true) {
        currentView = 'home';
        if (clearMsg) {
            message = '';
        }
    }

    function goToBorrow() {
        currentView = 'borrow';
        showCustomDuration = false;
        loadSavedContact();
        // No need to load items since users can enter any item name
    }

    function goToReturn() {
        currentView = 'return';
        loadActiveLoans();
    }

    function goToPrinterGuide() {
        currentView = 'printer-guide';
        showWifiPassword = false;
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

    <!-- Home View - Three Main Options -->
    {#if currentView === 'home'}
        <div class="home-options">
            <h2>What would you like to do?</h2>
            <div class="option-buttons">
                <button class="option-btn borrow-btn" style="--i: 0" on:click={goToBorrow}>
                    <span class="tile-icon" aria-hidden="true">📦</span>
                    <span class="tile-body">
                        <span class="tile-title">Borrow Item</span>
                        <span class="tile-sub">Take equipment from the lab</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </button>

                <button class="option-btn return-btn" style="--i: 1" on:click={goToReturn}>
                    <span class="tile-icon" aria-hidden="true">↩️</span>
                    <span class="tile-body">
                        <span class="tile-title">Return Item</span>
                        <span class="tile-sub">Give equipment back</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </button>

                <a class="option-btn printers-btn" style="--i: 2" href="/printers">
                    <img class="tile-image" src="/P1S.png" alt="" aria-hidden="true" />
                    <span class="tile-body">
                        <span class="tile-title">3D Printers</span>
                        <span class="tile-sub">See which printers are free</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </a>

                <a class="option-btn mocap-btn" style="--i: 3" href="/mocap">
                    <span class="tile-icon" aria-hidden="true">🎥</span>
                    <span class="tile-body">
                        <span class="tile-title">Motion Capture Lab</span>
                        <span class="tile-sub">Book a slot, see the calendar</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
                </a>

                <button class="option-btn printer-btn" style="--i: 4" on:click={goToPrinterGuide}>
                    <span class="tile-icon" aria-hidden="true">📖</span>
                    <span class="tile-body">
                        <span class="tile-title">Printer Guidelines</span>
                        <span class="tile-sub">How to print, safely</span>
                    </span>
                    <span class="tile-arrow" aria-hidden="true">→</span>
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
                        placeholder="Enter your name"
                    />
                    <small class="help-text">Your name and phone are saved on this device for next time.</small>
                </div>

                <div class="form-group">
                    <label for="phone">Phone Number *</label>
                    <input 
                        type="text" 
                        id="phone" 
                        bind:value={borrowForm.borrower_phone} 
                        required
                        maxlength="10"
                        placeholder="Enter 10-digit phone number"
                        title="Please enter exactly 10 digits"
                        inputmode="numeric"
                        on:input={(e) => {
                            // Remove any non-digit characters and update
                            const cleaned = e.target.value.replace(/[^0-9]/g, '');
                            borrowForm.borrower_phone = cleaned;
                            e.target.value = cleaned;
                            
                            // Custom validation
                            if (cleaned.length === 10) {
                                e.target.setCustomValidity('');
                            } else {
                                e.target.setCustomValidity('Please enter exactly 10 digits');
                            }
                        }}
                        on:blur={(e) => {
                            // Validate on blur as well
                            const cleaned = e.target.value.replace(/[^0-9]/g, '');
                            if (cleaned.length === 10) {
                                e.target.setCustomValidity('');
                            } else {
                                e.target.setCustomValidity('Please enter exactly 10 digits');
                            }
                        }}
                    />
                </div>

                <div class="form-group">
                    <label for="item">Item Name *</label>
                    <input 
                        type="text" 
                        id="item" 
                        bind:value={borrowForm.item_name} 
                        required
                        placeholder="Enter item name"
                    />
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
                    <div class="chip-row">
                        {#each durationPresets as preset}
                            <button
                                type="button"
                                class="chip"
                                class:selected={!showCustomDuration && borrowForm.return_days === preset.days && borrowForm.return_hours === preset.hours}
                                on:click={() => setDuration(preset)}
                            >
                                {preset.label}
                            </button>
                        {/each}
                        <button
                            type="button"
                            class="chip"
                            class:selected={showCustomDuration}
                            on:click={() => showCustomDuration = !showCustomDuration}
                        >
                            Custom
                        </button>
                    </div>
                    {#if showCustomDuration}
                        <div class="time-inputs">
                            <div class="time-input">
                                <label for="days">Days:</label>
                                <input 
                                    type="number" 
                                    id="days" 
                                    bind:value={borrowForm.return_days} 
                                    min="0" 
                                    max="30"
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
                    {/if}
                    <small class="help-text">
                        Expected return: {borrowForm.return_days} day{borrowForm.return_days !== 1 ? 's' : ''} 
                        {#if borrowForm.return_hours > 0}and {borrowForm.return_hours} hour{borrowForm.return_hours !== 1 ? 's' : ''}{/if}
                        from now
                    </small>
                </div>

                <div class="form-group">
                    <label for="purpose">Purpose <span class="optional-tag">optional</span></label>
                    <div class="chip-row">
                        {#each purposePresets as preset}
                            <button
                                type="button"
                                class="chip"
                                class:selected={borrowForm.purpose === preset}
                                on:click={() => borrowForm.purpose = borrowForm.purpose === preset ? '' : preset}
                            >
                                {preset}
                            </button>
                        {/each}
                    </div>
                    <textarea 
                        id="purpose" 
                        bind:value={borrowForm.purpose} 
                        placeholder="Anything more specific? (project name, priority)"
                        rows="2"
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
                <label for="search">Search by name, item, or phone number:</label>
                <div class="search-input-group">
                    <input 
                        type="text" 
                        id="search" 
                        bind:value={searchQuery}
                        placeholder="Enter name, item name, or phone number..."
                        autocomplete="off"
                    />
                    {#if searchQuery && searchQuery.trim()}
                        <button 
                            class="clear-search-btn" 
                            on:click={clearSearch} 
                            title="Clear search"
                            type="button"
                        >
                            ✕
                        </button>
                    {/if}
                </div>
            </div>

            {#if loading}
                <div class="loading-state">
                    <p>🔄 Loading items...</p>
                </div>
            {:else if filteredLoans.length === 0}
                <div class="no-items-state">
                    <p>📭 No items found.</p>
                    {#if searchQuery.trim()}
                        <p class="search-suggestion">Try different search terms or clear the search to see all items.</p>
                    {:else}
                        <p class="search-suggestion">No borrowed items available.</p>
                    {/if}
                </div>
            {:else}
                <!-- Simple list with category tags -->
                <div class="items-list">
                    {#if searchQuery.trim()}
                        <h3 class="results-header">Search Results ({filteredLoans.length} found)</h3>
                    {:else}
                        <h3 class="results-header">All Items ({filteredLoans.length} total)</h3>
                    {/if}
                    
                    {#each filteredLoans as loan (loan.ID)}
                        {@const itemCategory = loan.status === 'not_found' ? 'missing' :
                                              loan.status === 'returned' ? 'returned' : 'borrowed'}
                        
                        <div class="item-card {itemCategory}">
                            <!-- Category Tag -->
                            <div class="category-tag {itemCategory}">
                                {#if itemCategory === 'missing'}
                                    <span class="tag-icon">⚠️</span>
                                    <span class="tag-text">Missing Item</span>
                                {:else if itemCategory === 'borrowed'}
                                    <span class="tag-icon">📋</span>
                                    <span class="tag-text">Borrowed Item</span>
                                {:else if itemCategory === 'returned'}
                                    <span class="tag-icon">✅</span>
                                    <span class="tag-text">Returned Item</span>
                                {/if}
                            </div>

                            <!-- Item Content -->
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
                                            <strong>Quantity:</strong> {loan.quantity_borrowed}
                                        </div>
                                        <div class="detail-item">
                                            <strong>Phone:</strong> {loan.borrower_phone}
                                        </div>
                                    </div>

                                    <div class="detail-row">
                                        <div class="detail-item">
                                            <strong>Purpose:</strong> {loan.purpose}
                                        </div>
                                    </div>

                                    <div class="detail-row">
                                        <div class="detail-item">
                                            <strong>Requested:</strong> {formatDate(loan.CreatedAt)}
                                        </div>
                                        {#if loan.expected_return_date}
                                            <div class="detail-item">
                                                <strong>Expected Return:</strong> {formatExpectedReturn(loan.expected_return_date)}
                                            </div>
                                        {/if}
                                    </div>

                                    {#if loan.photo_filename}
                                        <div class="item-photo">
                                            <img 
                                                src="/api/photos/{loan.photo_filename}" 
                                                alt="{loan.item_name}"
                                                class="photo-thumbnail"
                                                loading="lazy"
                                            />
                                        </div>
                                    {/if}
                                </div>

                                <!-- Action Section -->
                                <div class="item-actions">
                                    {#if itemCategory === 'missing'}
                                        <p class="status-message missing">⚠️ This item has been marked as missing by an admin. Please contact lab personnel if found.</p>
                                    {:else if itemCategory === 'returned'}
                                        <p class="status-message returned">✅ Successfully returned.</p>
                                    {:else if itemCategory === 'borrowed'}
                                        <button 
                                            class="return-action-btn" 
                                            on:click={() => returnItem(loan.ID)}
                                            disabled={loading}
                                        >
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
            <h2>🖨️ Bambu Labs Printer Guidelines</h2>
            <button class="back-btn" on:click={goHome}>← Back to Home</button>
            
            <div class="guide-content">
                <!-- How to print, in the order you actually do it -->
                <section class="guide-section">
                    <h3>🚀 How to print - start here</h3>

                    <div class="rule-box">
                        <strong>The one rule: put your name in the file name.</strong>
                        <p>
                            Save your sliced file as <code>yourname_object.3mf</code> - for example
                            <code>srinath_bracket.3mf</code>. The printer reports the file name, so the
                            <a href="/printers">printer page</a> shows whose print is running on each
                            machine. A print nobody can identify is a print that gets stopped when
                            someone else needs the printer.
                        </p>
                    </div>

                    <ol class="steps">
                        <li>
                            <strong>Get on the printer network.</strong>
                            The printers have their own network inside the lab - they are not
                            reachable from wifi@iiith.
                            <div class="wifi-box">
                                <div class="wifi-row">
                                    <span class="wifi-label">Network</span>
                                    <code>printers@rrc</code>
                                </div>
                                <div class="wifi-row">
                                    <span class="wifi-label">Password</span>
                                    {#if showWifiPassword}
                                        <code>printers@1234</code>
                                        <button class="reveal-btn" on:click={() => showWifiPassword = false}>
                                            Hide
                                        </button>
                                    {:else}
                                        <code class="hidden-password">••••••••••••</code>
                                        <button class="reveal-btn" on:click={() => showWifiPassword = true}>
                                            Click to reveal
                                        </button>
                                    {/if}
                                </div>
                                <p class="wifi-note">
                                    Lab members only. Do not post this in group chats or share it
                                    outside the lab.
                                </p>
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
                            <div class="printer-table-wrap">
                                <table class="printer-table">
                                    <thead>
                                        <tr><th>Printer</th><th>Label on the machine</th><th>IP address</th></tr>
                                    </thead>
                                    <tbody>
                                        <tr><td>Printer 1</td><td>3DP-01P-279</td><td><code>192.168.2.101</code></td></tr>
                                        <tr><td>Printer 2</td><td>3DP-01P-112</td><td><code>192.168.2.105</code></td></tr>
                                        <tr><td>Printer 3</td><td>3DP-01P-739</td><td><code>192.168.2.102</code></td></tr>
                                    </tbody>
                                </table>
                            </div>
                            <span class="step-note">
                                Access codes are on each printer's screen. They are not listed here on
                                purpose - if a code stops working, tell an admin.
                            </span>
                        </li>
                        <li>
                            <strong>Slice with the right printer and the right material.</strong>
                            Select <em>Bambu Lab P1S</em>, and set the filament profile to the
                            material that is <em>actually loaded</em> in that machine.
                            <span class="danger-note">
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
                            send the print.
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

                <section class="guide-section">
                    <h3>🧵 Loading and unloading filament</h3>
                    <p class="section-intro">
                        Always use the printer's own screen menu. Never pull filament out cold and
                        never force it - that is how hotends get damaged.
                    </p>
                    <div class="two-col">
                        <div class="col-box">
                            <h4>Loading</h4>
                            <ol class="mini-steps">
                                <li>Put the spool on the holder so it feeds without tangling.</li>
                                <li>Snip the end at an angle so it feeds in cleanly.</li>
                                <li>Push it through the PTFE tube until it reaches the extruder.</li>
                                <li>On the screen, choose <em>Filament → Load</em> and pick the material.</li>
                                <li>Wait for the nozzle to heat and extrude, until the colour coming
                                    out is only your new filament.</li>
                            </ol>
                        </div>
                        <div class="col-box">
                            <h4>Unloading</h4>
                            <ol class="mini-steps">
                                <li>On the screen, choose <em>Filament → Unload</em>.</li>
                                <li>Wait for the nozzle to heat - this is not optional.</li>
                                <li>Let the printer retract it, then pull gently. If it resists,
                                    stop and ask an admin.</li>
                                <li>Clip the end and secure it on the spool so it does not tangle
                                    for the next person.</li>
                            </ol>
                        </div>
                    </div>
                    <p class="section-note">
                        Detailed official instructions are in the Bambu Lab documentation linked
                        below.
                    </p>
                </section>

                <section class="guide-section">
                    <h3>🤔 Questions about the printer page</h3>
                    <ul class="plain-list">
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

                <section class="guide-section">
                    <h3>🦺 Safety</h3>
                    <ul class="plain-list">
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

                <section class="guide-section">
                    <h3>📋 Lab rules</h3>
                    <ul class="plain-list">
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

                <!-- Official Documentation Links -->
                <section class="guide-section">
                    <h3>📚 Official Documentation</h3>
                    <div class="link-grid">
                        <a href="https://wiki.bambulab.com/en/p1/manual/loading-filament" target="_blank" rel="noopener noreferrer" class="guide-link">
                            <span class="link-icon">📥</span>
                            <div class="link-content">
                                <strong>Loading Filament</strong>
                                <p>Step-by-step guide to load filament into the printer</p>
                            </div>
                        </a>
                        
                        <a href="https://wiki.bambulab.com/en/p1/manual" target="_blank" rel="noopener noreferrer" class="guide-link">
                            <span class="link-icon">🔄</span>
                            <div class="link-content">
                                <strong>Replacing Filament</strong>
                                <p>How to change and replace filament during printing</p>
                            </div>
                        </a>
                        
                        <a href="https://wiki.bambulab.com/en/p1/manual/print-from-bambu-studio" target="_blank" rel="noopener noreferrer" class="guide-link">
                            <span class="link-icon">🚀</span>
                            <div class="link-content">
                                <strong>Sending Print Jobs</strong>
                                <p>Learn how to send print jobs from Bambu Studio</p>
                            </div>
                        </a>
                        
                        <a href="https://wiki.bambulab.com/en/general/filament-guide-material-table" target="_blank" rel="noopener noreferrer" class="guide-link">
                            <span class="link-icon">📖</span>
                            <div class="link-content">
                                <strong>Filament Guide</strong>
                                <p>Complete material table and filament reference</p>
                            </div>
                        </a>
                    </div>
                </section>

                <!-- Important Safety Guidelines -->
                <section class="guide-section warning-section">
                    <h3>⚠️ Important Safety Guidelines</h3>
                    <p class="section-intro">If you notice any of these issues during a print, <strong>STOP THE PRINT IMMEDIATELY</strong> and inform lab personnel:</p>
                    
                    <div class="warning-list">
                        <div class="warning-item">
                            <span class="warning-icon">🚫</span>
                            <div class="warning-content">
                                <strong>Nozzle Blockage</strong>
                                <p>If the nozzle appears blocked or no filament is extruding:</p>
                                <ul>
                                    <li>Stop the print immediately</li>
                                    <li>Do NOT attempt to clear it yourself</li>
                                    <li>Inform lab personnel right away</li>
                                </ul>
                            </div>
                        </div>
                        
                        <div class="warning-item">
                            <span class="warning-icon">🌀</span>
                            <div class="warning-content">
                                <strong>Tangled Filament / Printing in Air</strong>
                                <p>If the filament is tangled or the printer is printing in mid-air:</p>
                                <ul>
                                    <li>Stop the print immediately</li>
                                    <li>Do NOT touch the hot nozzle or bed</li>
                                    <li>Report the issue to lab personnel</li>
                                </ul>
                            </div>
                        </div>
                        
                        <div class="warning-item">
                            <span class="warning-icon">❌</span>
                            <div class="warning-content">
                                <strong>Print Failure / Messed Up Print</strong>
                                <p>If the print quality is poor or the print is failing:</p>
                                <ul>
                                    <li>Stop the print to avoid wasting material</li>
                                    <li>Take a photo of the issue if possible</li>
                                    <li>Notify lab personnel for assistance</li>
                                </ul>
                            </div>
                        </div>
                        
                        <div class="warning-item community-item">
                            <span class="warning-icon">👥</span>
                            <div class="warning-content">
                                <strong>Community Responsibility</strong>
                                <p><strong>Anyone can and should stop problematic prints!</strong></p>
                                <ul>
                                    <li>If you notice any issues on ANY print, take action</li>
                                    <li>It's everyone's responsibility to maintain the equipment</li>
                                    <li>Report stopped prints to lab personnel immediately</li>
                                    <li>Better to stop early than damage the printer</li>
                                </ul>
                            </div>
                        </div>
                    </div>
                </section>

                <!-- Emergency Contact -->
                <section class="guide-section contact-section">
                    <h3>📞 Need Help?</h3>
                    <p>If you encounter any issues or have questions, contact the lab personnel immediately.</p>
                    <p><strong>Remember:</strong> It's always better to stop a print and ask for help than to damage the equipment!</p>
                </section>
            </div>
        </div>
    {/if}
    </main>
</div>

<!-- Credits Footer -->
<footer class="credits-footer">
    <p>Created by <a href="https://github.com/Srindot" target="_blank"><strong>Srinath</strong></a></p>
    <p>Theme: <a href="https://github.com/catppuccin/catppuccin" target="_blank"><strong>Catppuccin Mocha</strong></a></p>
</footer>

<style>
    /* This page keeps its own inner scroll region, so the window itself does
       not scroll. Other routes reset these - see printers/mocap. */
    :global(body) {
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

    /* Once there is room, centre the choices in the space and let the tiles
       breathe - on a desktop they were a thin strip with a void beneath. */
    @media (min-width: 700px) and (min-height: 600px) {
        /* Let the choices fill whatever is left under the header, rather than
           100% of the parent - which starts below the header and overflowed,
           pushing everything low. */
        .main-content {
            display: flex;
            flex-direction: column;
        }

        .home-options {
            flex: 1;
            min-height: 0;
            display: flex;
            flex-direction: column;
            justify-content: center;
        }

        .option-btn {
            padding: 22px 24px;
            gap: 18px;
        }

        .tile-icon {
            font-size: 2.2rem;
        }

        .tile-image {
            width: 58px;
            height: 58px;
        }

        .tile-title {
            font-size: 1.2rem;
        }

        .tile-sub {
            font-size: 0.92rem;
        }
    }

    .option-buttons {
        display: grid;
        /* Deliberately capped: on a wide monitor four cramped columns look
           worse than two generous ones. */
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr));
        gap: clamp(12px, 1.6vw, 20px);
        margin-top: clamp(18px, 3vw, 28px);
        max-width: 780px;
        margin-left: auto;
        margin-right: auto;
    }

    /* Tiles are a row: icon, text, arrow. Reads well at any width. */
    .option-btn {
        --tile-accent: var(--ctp-lavender);
        position: relative;
        display: flex;
        align-items: center;
        gap: 14px;
        width: 100%;
        text-align: left;
        text-decoration: none;
        padding: clamp(14px, 2.4vw, 20px);
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-lg);
        background:
            linear-gradient(180deg, rgba(205, 214, 244, 0.04), transparent 60%),
            var(--ctp-mantle);
        color: var(--ctp-text);
        font-family: inherit;
        cursor: pointer;
        overflow: hidden;
        box-shadow: var(--shadow-sm);
        transition:
            transform var(--normal) var(--ease),
            border-color var(--normal) var(--ease),
            box-shadow var(--normal) var(--ease),
            background-color var(--normal) var(--ease);
        animation: rise var(--slow) var(--ease) both;
        animation-delay: calc(var(--i, 0) * 60ms);
    }

    /* A colour wash that grows from the left edge on hover */
    .option-btn::before {
        content: '';
        position: absolute;
        inset: 0;
        background: linear-gradient(
            90deg,
            color-mix(in srgb, var(--tile-accent) 18%, transparent),
            transparent 55%
        );
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
        transform: scaleY(0.25);
        transform-origin: center;
        transition: transform var(--normal) var(--ease);
    }

    .option-btn:hover,
    .option-btn:focus-visible {
        transform: translateY(-3px);
        border-color: color-mix(in srgb, var(--tile-accent) 55%, var(--ctp-surface1));
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
        filter: drop-shadow(0 2px 6px rgba(17, 17, 27, 0.5));
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
        letter-spacing: 0.1px;
        color: var(--tile-accent);
    }

    .tile-sub {
        font-size: clamp(0.8rem, 1.6vw, 0.88rem);
        color: var(--ctp-subtext0);
        line-height: 1.35;
    }

    .tile-arrow {
        color: var(--ctp-overlay1);
        font-size: 1.1rem;
        flex-shrink: 0;
        transform: translateX(-4px);
        opacity: 0;
        transition:
            transform var(--normal) var(--ease),
            opacity var(--normal) var(--ease),
            color var(--normal) var(--ease);
    }

    .option-btn:hover .tile-arrow,
    .option-btn:focus-visible .tile-arrow {
        opacity: 1;
        transform: none;
        color: var(--tile-accent);
    }

    .borrow-btn { --tile-accent: var(--ctp-flamingo); }
    .return-btn { --tile-accent: var(--ctp-sapphire); }
    .printers-btn { --tile-accent: var(--ctp-peach); }
    .mocap-btn { --tile-accent: var(--ctp-mauve); }
    .printer-btn { --tile-accent: var(--ctp-green); }

    .label-row {
        display: flex;
        justify-content: space-between;
        align-items: baseline;
        gap: 8px;
    }

    .link-btn {
        background: none;
        border: none;
        color: #89b4fa;
        font-size: 0.8rem;
        cursor: pointer;
        padding: 0;
        text-decoration: underline;
    }

    .optional-tag {
        font-size: 0.75rem;
        color: #6c7086;
        font-weight: 400;
        text-transform: none;
    }

    .chip-row {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-bottom: 8px;
    }

    .chip {
        background: #313244;
        color: #cdd6f4;
        border: 1px solid #45475a;
        border-radius: 999px;
        padding: 10px 16px;
        /* Comfortable one-handed tap target on a phone */
        min-height: 44px;
        font-size: 0.95rem;
        cursor: pointer;
    }

    .chip:hover {
        background: #45475a;
    }

    .chip.selected {
        background: #f2cdcd;
        border-color: #f2cdcd;
        color: #11111b;
        font-weight: 600;
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

    .search-input-group {
        position: relative;
        margin-top: 5px;
    }

    .search-container input {
        width: 100%;
        padding: clamp(12px, 3vw, 16px);
        padding-right: 50px; /* Make room for clear button */
        border: 2px solid #313244;
        border-radius: 8px;
        font-size: clamp(0.9rem, 2vw, 1rem);
        background: #1e1e2e;
        color: #cdd6f4;
        transition: all 0.3s ease;
        box-sizing: border-box;
        z-index: 1;
        position: relative;
    }

    .search-container input:focus {
        border-color: #f2cdcd;
        outline: none;
        box-shadow: 0 0 0 3px rgba(242, 205, 205, 0.25);
        z-index: 2;
    }

    .clear-search-btn {
        position: absolute;
        right: 12px;
        top: 50%;
        transform: translateY(-50%);
        background: #f38ba8;
        color: #1e1e2e;
        border: none;
        border-radius: 50%;
        width: 26px;
        height: 26px;
        display: flex;
        align-items: center;
        justify-content: center;
        cursor: pointer;
        font-size: 14px;
        font-weight: bold;
        transition: all 0.3s ease;
        z-index: 3;
        user-select: none;
        flex-shrink: 0;
    }

    .clear-search-btn:hover {
        background: #f2cdcd;
        transform: translateY(-50%) scale(1.1);
    }

    /* Search Results Styles */
    .search-results h3 {
        color: #cdd6f4;
        margin: 20px 0 15px 0;
        font-size: clamp(1.1rem, 2.5vw, 1.3rem);
        border-left: 4px solid #f38ba8;
        padding-left: 15px;
    }

    .search-results .loan-card {
        margin-bottom: 15px;
    }

    .search-results .status-badge {
        display: inline-block;
        padding: 4px 12px;
        border-radius: 20px;
        font-size: 0.8rem;
        font-weight: bold;
        margin-left: 10px;
    }

    .search-results .status-badge.lost {
        background: #f38ba8;
        color: #1e1e2e;
    }

    .search-results .status-badge.overdue {
        background: #fab387;
        color: #1e1e2e;
    }

    .search-results .status-badge.pending {
        background: #f9e2af;
        color: #1e1e2e;
    }

    .search-results .status-badge.borrowed {
        background: #89b4fa;
        color: #1e1e2e;
    }

    .search-results .status-badge.returned {
        background: #a6e3a1;
        color: #1e1e2e;
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

    .loan-content {
        display: flex;
        align-items: flex-start;
        gap: 15px;
        width: 100%;
    }

    .item-image {
        flex-shrink: 0;
        width: 80px;
        height: 80px;
        border-radius: 8px;
        overflow: hidden;
        background: #181825;
        border: 2px solid #313244;
        display: flex;
        align-items: center;
        justify-content: center;
    }

    .item-image img.loan-thumbnail {
        width: 100%;
        height: 100%;
        object-fit: cover;
        transition: transform 0.2s ease;
    }

    .item-image:hover img.loan-thumbnail {
        transform: scale(1.05);
    }

    .item-image.placeholder {
        flex-direction: column;
        color: #6c7086;
        font-size: 24px;
        text-align: center;
    }

    .item-image.placeholder small {
        font-size: 10px;
        margin-top: 2px;
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

        .loan-content {
            flex-direction: column;
            align-items: center;
            gap: 15px;
        }

        .item-image {
            width: 100px;
            height: 100px;
        }

        .loan-info {
            width: 100%;
            margin-bottom: 15px;
            text-align: center;
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
            max-width: 820px;
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
            max-width: 880px;
        }
    }

    /* Status-based loan card styles */
    .loan-card.missing {
        border: 2px solid #f38ba8;
        background: rgba(243, 139, 168, 0.1);
    }

    .loan-card.overdue {
        border: 2px solid #fab387;
        background: rgba(250, 179, 135, 0.1);
    }

    .loan-card.pending {
        border: 2px solid #f9e2af;
        background: rgba(249, 226, 175, 0.08);
    }

    .loan-card.approved {
        border: 2px solid #89b4fa;
        background: rgba(137, 180, 250, 0.08);
    }

    .loan-card.returned {
        border: 2px solid #a6e3a1;
        background: rgba(166, 227, 161, 0.08);
    }

    .loan-card.denied {
        border: 2px solid #6c7086;
        background: rgba(108, 112, 134, 0.08);
        opacity: 0.8;
    }

    /* Status header and badges */
    .status-header {
        margin-bottom: 15px;
        display: flex;
        justify-content: flex-start;
    }

    .status-badge {
        padding: 6px 12px;
        border-radius: 6px;
        font-weight: bold;
        font-size: clamp(0.8rem, 1.8vw, 0.9rem);
        text-transform: uppercase;
        letter-spacing: 0.5px;
    }

    .status-badge.missing {
        background: #f38ba8;
        color: #11111b;
    }

    .status-badge.overdue {
        background: #fab387;
        color: #11111b;
    }

    .status-badge.pending {
        background: #f9e2af;
        color: #11111b;
    }

    .status-badge.approved {
        background: #89b4fa;
        color: #11111b;
    }

    .status-badge.returned {
        background: #a6e3a1;
        color: #11111b;
    }

    .status-badge.denied {
        background: #6c7086;
        color: #cdd6f4;
    }

    .status-badge.return-pending {
        background: #fab387;
        color: #11111b;
    }

    .status-badge.return-pending {
        background: #fab387;
        color: #1e1e2e;
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
        color: #a6e3a1;
        font-weight: 500;
        margin-top: 10px;
    }

    .returned-actions {
        padding: 15px;
        background: rgba(166, 227, 161, 0.15);
        border-radius: 6px;
        text-align: center;
    }

    .returned-message {
        color: #a6e3a1;
        font-weight: 600;
        margin: 0;
        font-size: 1.1rem;
    }

    /* Collapsible Categories Styles */
    .category-section {
        margin-bottom: 20px;
        border-radius: 12px;
        overflow: hidden;
        box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
    }

    .category-header {
        width: 100%;
        background: #313244;
        border: none;
        padding: 16px 20px;
        display: flex;
        align-items: center;
        justify-content: space-between;
        cursor: pointer;
        transition: all 0.3s ease;
        font-family: inherit;
        font-size: 1.1rem;
        font-weight: 600;
    }

    .category-header:hover {
        background: #45475a;
        transform: translateY(-1px);
    }

    .category-header.lost {
        background: linear-gradient(135deg, #f38ba8, #eba0ac);
        color: #11111b;
    }

    .category-header.overdue {
        background: linear-gradient(135deg, #fab387, #f2cdcd);
        color: #11111b;
    }

    .category-header.pending {
        background: linear-gradient(135deg, #f9e2af, #f2cdcd);
        color: #11111b;
    }

    .category-header.borrowed {
        background: linear-gradient(135deg, #89b4fa, #b4befe);
        color: #11111b;
    }

    .category-header.returned {
        background: linear-gradient(135deg, #a6e3a1, #94e2d5);
        color: #11111b;
    }

    .category-title {
        display: flex;
        align-items: center;
        gap: 12px;
    }

    .category-icon {
        font-size: 1.3rem;
    }

    .category-name {
        font-size: 1.1rem;
        font-weight: 600;
    }

    .category-count {
        font-size: 0.9rem;
        opacity: 0.8;
        font-weight: 500;
    }

    .collapse-icon {
        font-size: 1.2rem;
        transition: transform 0.3s ease;
    }

    .collapse-icon.collapsed {
        transform: rotate(-90deg);
    }

    .category-content {
        background: #181825;
        padding: 0;
    }

    .category-content .loan-card {
        margin: 0;
        border-radius: 0;
        border-left: none;
        border-right: none;
        border-top: 1px solid #313244;
    }

    .category-content .loan-card:last-child {
        border-bottom: none;
    }

    /* Pending actions styling */
    .pending-actions {
        padding: 15px;
        background: rgba(249, 226, 175, 0.15);
        border-radius: 6px;
        text-align: center;
    }

    .pending-message {
        color: #f9e2af;
        font-weight: 600;
        margin: 0;
        font-size: 1rem;
    }

    /* New Simple List Styles */
    .loading-state, .no-items-state {
        text-align: center;
        padding: 40px 20px;
        background: #11111b;
        border-radius: 12px;
        margin-top: 20px;
        border: 1px solid #313244;
    }

    .loading-state p, .no-items-state p {
        font-size: 1.2rem;
        margin: 0;
        color: #cdd6f4;
    }

    .search-suggestion {
        color: #a6adc8 !important;
        font-size: 1rem !important;
        margin-top: 10px !important;
    }

    .items-list {
        margin-top: 20px;
    }

    .results-header {
        background: #11111b;
        padding: 15px 20px;
        margin: 0 0 15px 0;
        border-radius: 8px;
        border: 1px solid #313244;
        color: #cdd6f4;
        font-size: 1.1rem;
        font-weight: 600;
    }

    .item-card {
        background: #11111b;
        border: 1px solid #313244;
        border-radius: 12px;
        margin-bottom: 15px;
        overflow: hidden;
        transition: all 0.3s ease;
        box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
    }

    .item-card:hover {
        border-color: #f2cdcd;
        transform: translateY(-2px);
        box-shadow: 0 4px 15px rgba(0, 0, 0, 0.3);
    }

    .item-card.missing {
        border-left: 4px solid #f38ba8;
    }

    .item-card.borrowed {
        border-left: 4px solid #89b4fa;
    }

    .item-card.pending {
        border-left: 4px solid #f9e2af;
    }

    .item-card.returned {
        border-left: 4px solid #a6e3a1;
    }

    .category-tag {
        padding: 8px 15px;
        font-weight: 600;
        font-size: 0.9rem;
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .category-tag.missing {
        background: rgba(243, 139, 168, 0.15);
        color: #f38ba8;
    }

    .category-tag.borrowed {
        background: rgba(137, 180, 250, 0.15);
        color: #89b4fa;
    }

    .category-tag.pending {
        background: rgba(249, 226, 175, 0.15);
        color: #f9e2af;
    }

    .category-tag.returned {
        background: rgba(166, 227, 161, 0.15);
        color: #a6e3a1;
    }

    .tag-icon {
        font-size: 1rem;
    }

    .tag-text {
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.5px;
    }

    .item-content {
        padding: 20px;
    }

    .item-header {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        margin-bottom: 15px;
        flex-wrap: wrap;
        gap: 10px;
    }

    .item-name {
        margin: 0;
        color: #cdd6f4;
        font-size: 1.3rem;
        font-weight: 600;
        flex: 1;
        min-width: 200px;
    }

    .return-pending-badge {
        background: rgba(116, 199, 236, 0.15);
        color: #74c7ec;
        padding: 4px 12px;
        border-radius: 20px;
        font-size: 0.85rem;
        font-weight: 600;
        white-space: nowrap;
    }

    .item-details {
        margin-bottom: 20px;
    }

    .detail-row {
        display: flex;
        gap: 20px;
        margin-bottom: 12px;
        flex-wrap: wrap;
    }

    .detail-item {
        flex: 1;
        min-width: 200px;
        color: #a6adc8;
        font-size: 0.95rem;
    }

    .detail-item strong {
        color: #cdd6f4;
        font-weight: 600;
    }

    .item-photo {
        margin-top: 15px;
        text-align: center;
    }

    .photo-thumbnail {
        max-width: 300px;
        max-height: 200px;
        width: auto;
        height: auto;
        object-fit: cover;
        border-radius: 8px;
        border: 2px solid #313244;
        transition: transform 0.2s ease;
    }

    .photo-thumbnail:hover {
        transform: scale(1.05);
        border-color: #f2cdcd;
    }

    .item-actions {
        padding-top: 15px;
        border-top: 1px solid #313244;
        text-align: center;
    }

    .status-message {
        margin: 0;
        padding: 12px;
        border-radius: 8px;
        font-weight: 600;
        font-size: 1rem;
    }

    .status-message.missing {
        background: rgba(243, 139, 168, 0.15);
        color: #f38ba8;
        border: 1px solid rgba(243, 139, 168, 0.3);
    }

    .status-message.returned {
        background: rgba(166, 227, 161, 0.15);
        color: #a6e3a1;
        border: 1px solid rgba(166, 227, 161, 0.3);
    }

    .status-message.pending {
        background: rgba(249, 226, 175, 0.15);
        color: #f9e2af;
        border: 1px solid rgba(249, 226, 175, 0.3);
    }

    .status-message.return-pending {
        background: rgba(116, 199, 236, 0.15);
        color: #74c7ec;
        border: 1px solid rgba(116, 199, 236, 0.3);
    }

    .return-action-btn {
        background: linear-gradient(135deg, #a6e3a1, #94e2d5);
        color: #11111b;
        border: none;
        padding: 12px 24px;
        border-radius: 8px;
        font-size: 1rem;
        font-weight: 600;
        cursor: pointer;
        transition: all 0.3s ease;
        display: inline-flex;
        align-items: center;
        gap: 8px;
    }

    .return-action-btn:hover {
        background: linear-gradient(135deg, #94e2d5, #89dceb);
        transform: translateY(-2px);
        box-shadow: 0 4px 12px rgba(166, 227, 161, 0.3);
    }

    .return-action-btn:disabled {
        background: #45475a;
        color: #6c7086;
        cursor: not-allowed;
        transform: none;
        box-shadow: none;
    }

    .pending-actions {
        display: flex;
        flex-direction: column;
        gap: 15px;
        align-items: center;
    }

    .cancel-btn {
        background: linear-gradient(135deg, #f38ba8, #eba0ac) !important;
        color: #11111b !important;
    }

    .cancel-btn:hover {
        background: linear-gradient(135deg, #eba0ac, #f9e2af) !important;
        transform: translateY(-2px);
        box-shadow: 0 4px 12px rgba(243, 139, 168, 0.3);
    }

    /* Responsive adjustments for new design */
    @media (max-width: 768px) {
        .item-header {
            flex-direction: column;
            align-items: stretch;
        }

        .item-name {
            min-width: auto;
            margin-bottom: 8px;
        }

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
    }

    /* Printer Guide Styles */
    .wifi-box {
        background: #181825;
        border: 1px solid #313244;
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
        color: #a6adc8;
        font-size: 0.8rem;
        min-width: 70px;
    }

    .hidden-password {
        letter-spacing: 2px;
        color: #6c7086;
    }

    .reveal-btn {
        background: #313244;
        color: #cdd6f4;
        border: 1px solid #45475a;
        border-radius: 6px;
        padding: 8px 14px;
        /* Big enough to tap on a phone */
        min-height: 38px;
        font-size: 0.8rem;
        cursor: pointer;
    }

    .reveal-btn:hover {
        background: #45475a;
    }

    .wifi-note {
        margin: 4px 0 0;
        font-size: 0.75rem;
        color: #6c7086;
    }

    .danger-note {
        display: block;
        margin-top: 6px;
        padding: 8px 10px;
        background: #3b2a2a;
        border-left: 3px solid #f38ba8;
        border-radius: 4px;
        font-size: 0.85rem;
        color: #f5c2c7;
    }

    .section-intro {
        font-size: 0.9rem;
        color: #a6adc8;
        margin: 0 0 10px;
    }

    .section-note {
        font-size: 0.8rem;
        color: #6c7086;
        margin-top: 8px;
    }

    .two-col {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
        gap: 12px;
    }

    .col-box {
        background: #181825;
        border: 1px solid #313244;
        border-radius: 8px;
        padding: 10px 12px;
    }

    .col-box h4 {
        margin: 0 0 8px;
        color: #94e2d5;
        font-size: 0.95rem;
    }

    .mini-steps {
        padding-left: 18px;
        margin: 0;
    }

    .mini-steps li {
        margin-bottom: 6px;
        font-size: 0.85rem;
        line-height: 1.45;
    }

    .rule-box {
        background: #2a2438;
        border: 1px solid #cba6f7;
        border-left: 4px solid #cba6f7;
        border-radius: 8px;
        padding: 14px 16px;
        margin-bottom: 18px;
    }

    .rule-box strong {
        color: #cba6f7;
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

    .steps li {
        margin-bottom: 14px;
        line-height: 1.5;
        font-size: 0.92rem;
    }

    .steps strong {
        color: #f9e2af;
    }

    .step-note {
        display: block;
        margin-top: 6px;
        font-size: 0.82rem;
        color: #a6adc8;
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

    .printer-table-wrap {
        overflow-x: auto;
        margin: 10px 0 4px;
    }

    .printer-table {
        border-collapse: collapse;
        font-size: 0.85rem;
        min-width: 380px;
    }

    .printer-table th,
    .printer-table td {
        border: 1px solid #313244;
        padding: 6px 10px;
        text-align: left;
    }

    .printer-table th {
        background: #181825;
        color: #a6adc8;
        font-weight: 600;
    }

    .guide-content code {
        background: #181825;
        border: 1px solid #313244;
        border-radius: 4px;
        padding: 1px 6px;
        font-size: 0.88em;
    }

    .guide-content a {
        color: #89b4fa;
    }

    .printer-guide-container {
        background: #11111b;
        padding: clamp(15px, 3vw, 25px);
        border-radius: 12px;
        margin-top: 15px;
        border: 1px solid #313244;
        box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
        max-width: 1200px;
        margin-left: auto;
        margin-right: auto;
    }

    .guide-content {
        margin-top: 25px;
    }

    .guide-section {
        background: #181825;
        padding: clamp(18px, 3.5vw, 28px);
        border-radius: 10px;
        margin-bottom: 25px;
        border: 1px solid #313244;
    }

    .guide-section h3 {
        color: #f2cdcd;
        margin-bottom: 20px;
        font-size: clamp(1.2rem, 2.5vw, 1.5rem);
    }

    .section-intro {
        color: #cdd6f4;
        margin-bottom: 20px;
        font-size: clamp(0.95rem, 2vw, 1.05rem);
        line-height: 1.6;
    }

    .link-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
        gap: 15px;
    }

    .guide-link {
        display: flex;
        align-items: flex-start;
        gap: 15px;
        padding: 18px;
        background: #1e1e2e;
        border: 2px solid #313244;
        border-radius: 8px;
        text-decoration: none;
        color: #cdd6f4;
        transition: all 0.3s ease;
    }

    .guide-link:hover {
        border-color: #a6e3a1;
        background: #262637;
        transform: translateY(-3px);
        box-shadow: 0 8px 16px rgba(166, 227, 161, 0.2);
    }

    .link-icon {
        font-size: 2rem;
        flex-shrink: 0;
    }

    .link-content strong {
        color: #a6e3a1;
        font-size: clamp(1rem, 2.2vw, 1.1rem);
        display: block;
        margin-bottom: 6px;
    }

    .link-content p {
        color: #a6adc8;
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
        line-height: 1.4;
        margin: 0;
    }

    .warning-section {
        border: 2px solid #f38ba8;
        background: linear-gradient(135deg, rgba(243, 139, 168, 0.08), rgba(235, 160, 172, 0.05));
    }

    .warning-section h3 {
        color: #f38ba8;
    }

    .warning-list {
        display: flex;
        flex-direction: column;
        gap: 20px;
    }

    .warning-item {
        display: flex;
        align-items: flex-start;
        gap: 18px;
        padding: 20px;
        background: #1e1e2e;
        border-left: 4px solid #f38ba8;
        border-radius: 8px;
    }

    .community-item {
        border-left-color: #a6e3a1;
        background: rgba(166, 227, 161, 0.08);
    }

    .warning-icon {
        font-size: 2.5rem;
        flex-shrink: 0;
        line-height: 1;
    }

    .warning-content strong {
        color: #f2cdcd;
        font-size: clamp(1.05rem, 2.3vw, 1.15rem);
        display: block;
        margin-bottom: 10px;
    }

    .community-item .warning-content strong {
        color: #a6e3a1;
    }

    .warning-content p {
        color: #cdd6f4;
        margin: 8px 0;
        line-height: 1.6;
        font-size: clamp(0.9rem, 1.9vw, 1rem);
    }

    .warning-content ul {
        margin: 12px 0 0 20px;
        padding: 0;
        color: #a6adc8;
    }

    .warning-content li {
        margin-bottom: 8px;
        line-height: 1.5;
        font-size: clamp(0.85rem, 1.8vw, 0.95rem);
    }

    .contact-section {
        background: linear-gradient(135deg, rgba(137, 180, 250, 0.1), rgba(180, 190, 254, 0.08));
        border: 2px solid #89b4fa;
        text-align: center;
    }

    .contact-section h3 {
        color: #89b4fa;
    }

    .contact-section p {
        color: #cdd6f4;
        font-size: clamp(0.95rem, 2vw, 1.05rem);
        line-height: 1.7;
        margin: 10px 0;
    }

    .contact-section strong {
        color: #f2cdcd;
    }

    @media (max-width: 768px) {
        .link-grid {
            grid-template-columns: 1fr;
        }

        .guide-link {
            padding: 15px;
        }

        .warning-item {
            flex-direction: column;
            gap: 12px;
            padding: 15px;
        }

        .warning-icon {
            font-size: 2rem;
        }
    }
</style>