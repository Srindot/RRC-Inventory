<script>
    import { onMount, onDestroy } from 'svelte';

    const STATUS_INTERVAL = 5000;   // printers report every few seconds
    const CAMERA_INTERVAL = 2000;   // the camera itself only manages ~0.5 fps

    let printers = [];
    let loaded = false;
    let error = '';
    let notice = '';
    let stopping = '';
    // Stopping a print is admin-only; the button only appears when an admin
    // session is present, and the backend checks the token regardless.
    let adminToken = '';
    // Printer id currently showing the "change access code" box
    let editingCode = '';
    let newCode = '';
    let savingCode = false;
    let cameraTick = Date.now();
    let statusTimer;
    let cameraTimer;

    onMount(() => {
        adminToken = localStorage.getItem('adminToken') || '';

        loadPrinters();
        statusTimer = setInterval(loadPrinters, STATUS_INTERVAL);
        cameraTimer = setInterval(() => (cameraTick = Date.now()), CAMERA_INTERVAL);
    });

    onDestroy(() => {
        clearInterval(statusTimer);
        clearInterval(cameraTimer);
    });

    async function loadPrinters() {
        try {
            const response = await fetch('/api/printers');
            if (response.ok) {
                printers = await response.json();
                error = '';
            } else {
                error = 'Could not load printer status';
            }
        } catch (e) {
            error = 'Could not reach the server';
        } finally {
            loaded = true;
        }
    }

    async function stopPrint(printer) {
        const job = printer.file_name ? tidyFileName(printer.file_name) : 'the current job';
        if (!confirm(`Stop ${job} on ${printer.name}?\n\nThis cannot be undone - the print will be cancelled.`)) {
            return;
        }

        stopping = printer.id;
        try {
            const response = await fetch(`/api/admin/printers/${printer.id}/stop`, {
                method: 'POST',
                headers: { Authorization: `Bearer ${adminToken}` }
            });

            if (response.status === 401) {
                adminToken = '';
                error = 'Your admin session expired. Log in again to stop prints.';
                return;
            }

            const result = await response.json();
            if (response.ok) {
                notice = `Stop command sent to ${printer.name}`;
                setTimeout(() => (notice = ''), 6000);
                loadPrinters();
            } else {
                error = result.error || 'Could not stop the print';
                setTimeout(() => (error = ''), 6000);
            }
        } catch (e) {
            error = 'Could not reach the server';
        } finally {
            stopping = '';
        }
    }

    function startCodeEdit(printer) {
        editingCode = printer.id;
        newCode = '';
    }

    async function saveAccessCode(printer) {
        if (!newCode.trim()) {
            error = 'Enter the new access code from the printer screen';
            return;
        }

        savingCode = true;
        try {
            const response = await fetch(`/api/admin/printers/${printer.id}/access-code`, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json',
                    Authorization: `Bearer ${adminToken}`
                },
                body: JSON.stringify({ access_code: newCode.trim() })
            });

            if (response.status === 401) {
                adminToken = '';
                error = 'Your admin session expired. Log in again.';
                return;
            }

            const result = await response.json();
            if (response.ok) {
                notice = `${printer.name}: access code updated, reconnecting...`;
                setTimeout(() => (notice = ''), 8000);
                editingCode = '';
                newCode = '';
                // Reconnection takes a few seconds
                setTimeout(loadPrinters, 4000);
                setTimeout(loadPrinters, 12000);
            } else {
                error = result.error || 'Could not update the access code';
            }
        } catch (e) {
            error = 'Could not reach the server';
        } finally {
            savingCode = false;
        }
    }

    // A printer is "busy" only while actually running a job
    function isPrinting(printer) {
        return printer.online && printer.state === 'RUNNING';
    }

    function stateLabel(printer) {
        if (!printer.online) return 'Offline';
        switch (printer.state) {
            case 'RUNNING': return 'Printing';
            case 'FINISH': return 'Finished';
            case 'FAILED': return 'Failed';
            case 'PAUSE': return 'Paused';
            case 'IDLE': return 'Idle';
            case 'PREPARE': return 'Preparing';
            case 'SLICING': return 'Slicing';
            default: return printer.state || 'Idle';
        }
    }

    function stateClass(printer) {
        if (!printer.online) return 'offline';
        switch (printer.state) {
            case 'RUNNING': return 'running';
            case 'FAILED': return 'failed';
            case 'PAUSE': return 'paused';
            case 'FINISH': return 'finished';
            default: return 'idle';
        }
    }

    // "Free" is what most people actually want to know
    function availability(printer) {
        if (!printer.online) return 'Unknown';
        return isPrinting(printer) ? 'In use' : 'Free';
    }

    function formatRemaining(minutes) {
        if (!minutes || minutes <= 0) return '';
        if (minutes < 60) return `${minutes} min left`;
        const hours = Math.floor(minutes / 60);
        const rest = minutes % 60;
        return rest ? `${hours}h ${rest}m left` : `${hours}h left`;
    }

    // Slicers add a lot of noise to file names
    function tidyFileName(name) {
        if (!name) return '';
        return name.replace(/\.gcode\.3mf$/i, '').replace(/\.3mf$/i, '');
    }
</script>

<div class="container">
    <div class="header">
        <div class="logo-title">
            <a href="/"><img src="/rrc_logo.png" alt="RRC Logo" class="logo" /></a>
            <div>
                <h1>🖨️ 3D Printers</h1>
                <p class="subtitle">Live status of the lab's Bambu Lab P1S printers</p>
            </div>
        </div>
        <a href="/" class="back-link">← Back to Home</a>
    </div>

    {#if error}
        <div class="message error">{error}</div>
    {/if}
    {#if notice}
        <div class="message success">{notice}</div>
    {/if}

    {#if !loaded}
        <p class="note">Loading printers...</p>
    {:else if printers.length === 0}
        <p class="note">No printers are configured.</p>
    {:else}
        <div class="printer-grid">
            {#each printers as printer (printer.id)}
                <div class="printer-card {stateClass(printer)}">
                    <div class="card-head">
                        <img src="/P1S.png" alt="" class="printer-icon" />
                        <div class="head-text">
                            <h2>{printer.name}</h2>
                            <span class="availability {isPrinting(printer) ? 'busy' : 'free'}">
                                {availability(printer)}
                            </span>
                        </div>
                        <span class="state-badge {stateClass(printer)}">{stateLabel(printer)}</span>
                    </div>

                    {#if printer.access_code_problem}
                        <div class="code-warning">
                            <strong>⚠️ Access code changed</strong>
                            <p>
                                {printer.name} is refusing our access code. This happens when
                                LAN mode is toggled on the printer, which regenerates the code.
                            </p>
                            {#if adminToken}
                                {#if editingCode === printer.id}
                                    <div class="code-form">
                                        <input
                                            type="text"
                                            bind:value={newCode}
                                            placeholder="New access code from the printer screen"
                                            autocomplete="off"
                                        />
                                        <button
                                            class="save-code-btn"
                                            on:click={() => saveAccessCode(printer)}
                                            disabled={savingCode}
                                        >
                                            {savingCode ? 'Saving...' : 'Save'}
                                        </button>
                                        <button class="cancel-code-btn" on:click={() => (editingCode = '')}>
                                            Cancel
                                        </button>
                                    </div>
                                    <p class="code-hint">
                                        On the printer: Settings → Network → the code is shown with
                                        the IP address. No restart needed - it reconnects by itself.
                                    </p>
                                {:else}
                                    <button class="fix-code-btn" on:click={() => startCodeEdit(printer)}>
                                        Update access code
                                    </button>
                                {/if}
                            {:else}
                                <p class="code-hint">An admin can fix this from this page.</p>
                            {/if}
                        </div>
                    {/if}

                    <div class="camera">
                        {#if printer.camera_online}
                            <img
                                src="/api/printers/{printer.id}/snapshot?t={cameraTick}"
                                alt="Camera view of {printer.name}"
                            />
                        {:else}
                            <div class="camera-placeholder">
                                <img src="/P1S.png" alt="" class="placeholder-icon" />
                                <p>No camera image</p>
                            </div>
                        {/if}
                    </div>

                    {#if printer.online}
                        {#if isPrinting(printer)}
                            <div class="progress-row">
                                <div class="progress-bar">
                                    <div class="progress-fill" style="width: {printer.progress}%"></div>
                                </div>
                                <span class="progress-text">{printer.progress}%</span>
                            </div>
                            {#if printer.remaining_minutes > 0}
                                <p class="remaining">{formatRemaining(printer.remaining_minutes)}</p>
                            {/if}
                        {/if}

                        {#if printer.file_name}
                            <p class="file" title={printer.file_name}>
                                📄 {tidyFileName(printer.file_name)}
                            </p>
                        {/if}

                        <div class="temps">
                            <span>🔥 Nozzle {printer.nozzle_temp.toFixed(0)}°C</span>
                            <span>🛏️ Bed {printer.bed_temp.toFixed(0)}°C</span>
                            {#if printer.chamber_temp > 0}
                                <span>📦 Chamber {printer.chamber_temp.toFixed(0)}°C</span>
                            {/if}
                        </div>
                        {#if adminToken && isPrinting(printer)}
                            <button
                                class="stop-btn"
                                on:click={() => stopPrint(printer)}
                                disabled={stopping === printer.id}
                            >
                                {stopping === printer.id ? 'Stopping...' : '⏹ Stop Print'}
                            </button>
                        {/if}

                        {#if printer.last_action_by}
                            <p class="last-action">Last stopped by {printer.last_action_by}</p>
                        {/if}
                    {:else}
                        <p class="offline-note">
                            Not responding - it may be switched off or off the network.
                        </p>
                    {/if}
                </div>
            {/each}
        </div>

        <p class="footnote">
            Status refreshes automatically. The camera updates about once every two
            seconds - that is the fastest the P1S allows over the local network.
            {#if !adminToken}
                Stopping a print requires an <a href="/admin">admin login</a>.
            {/if}
        </p>
    {/if}
</div>

<style>
    :global(body) {
        background: #1e1e2e;
        color: #cdd6f4;
        font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
        margin: 0;
        /* The home page locks the page height with overflow:hidden. That style
           stays loaded after client-side navigation, so undo it here or this
           page cannot be scrolled. */
        height: auto;
        min-height: 100vh;
        overflow-y: auto;
    }

    :global(html) {
        height: auto;
        overflow-y: auto;
    }

    .container {
        max-width: 1200px;
        margin: 0 auto;
        padding: 16px;
    }

    .header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
        border-bottom: 1px solid #313244;
        padding-bottom: 12px;
        margin-bottom: 16px;
    }

    .logo-title {
        display: flex;
        align-items: center;
        gap: 12px;
    }

    .logo {
        height: 48px;
        width: auto;
    }

    h1 {
        margin: 0;
        font-size: clamp(1.2rem, 3vw, 1.6rem);
        color: #fab387;
    }

    .subtitle {
        margin: 2px 0 0;
        font-size: 0.85rem;
        color: #a6adc8;
    }

    .back-link {
        color: #89b4fa;
        text-decoration: none;
        font-size: 0.9rem;
    }

    .message.error {
        background: #3b2a2a;
        color: #f38ba8;
        border: 1px solid #f38ba8;
        padding: 10px 14px;
        border-radius: 8px;
        margin-bottom: 14px;
        font-size: 0.9rem;
    }

    .note,
    .footnote {
        color: #6c7086;
        font-size: 0.85rem;
        text-align: center;
        margin-top: 16px;
    }

    .printer-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
        gap: 16px;
    }

    .printer-card {
        background: #11111b;
        border: 1px solid #313244;
        border-left: 4px solid #6c7086;
        border-radius: 10px;
        padding: 14px;
    }

    .printer-card.running { border-left-color: #a6e3a1; }
    .printer-card.failed { border-left-color: #f38ba8; }
    .printer-card.paused { border-left-color: #f9e2af; }
    .printer-card.finished { border-left-color: #89b4fa; }
    .printer-card.offline { opacity: 0.6; }

    .card-head {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 10px;
        margin-bottom: 10px;
    }

    .printer-icon {
        width: 42px;
        height: 42px;
        object-fit: contain;
        flex-shrink: 0;
    }

    .head-text {
        flex: 1;
        min-width: 0;
    }

    h2 {
        margin: 0;
        font-size: 1rem;
    }

    .availability {
        font-size: 0.8rem;
        font-weight: 600;
    }

    .availability.free { color: #a6e3a1; }
    .availability.busy { color: #fab387; }

    .state-badge {
        font-size: 0.75rem;
        padding: 3px 9px;
        border-radius: 999px;
        background: #313244;
        color: #cdd6f4;
        white-space: nowrap;
    }

    .state-badge.running { background: #a6e3a1; color: #11111b; }
    .state-badge.failed { background: #f38ba8; color: #11111b; }
    .state-badge.paused { background: #f9e2af; color: #11111b; }
    .state-badge.finished { background: #89b4fa; color: #11111b; }

    .camera {
        aspect-ratio: 16 / 9;
        background: #181825;
        border-radius: 8px;
        overflow: hidden;
        margin-bottom: 10px;
    }

    .camera img {
        width: 100%;
        height: 100%;
        object-fit: cover;
        display: block;
    }

    .camera-placeholder {
        width: 100%;
        height: 100%;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        color: #45475a;
    }

    /* Scoped past the ".camera img" rule above, which would stretch it */
    .camera-placeholder .placeholder-icon {
        width: 64px;
        height: 64px;
        object-fit: contain;
        opacity: 0.35;
    }

    .camera-placeholder p {
        margin: 4px 0 0;
        font-size: 0.8rem;
    }

    .progress-row {
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .progress-bar {
        flex: 1;
        height: 8px;
        background: #313244;
        border-radius: 999px;
        overflow: hidden;
    }

    .progress-fill {
        height: 100%;
        background: linear-gradient(90deg, #a6e3a1, #94e2d5);
        transition: width 0.4s ease;
    }

    .progress-text {
        font-size: 0.8rem;
        color: #a6adc8;
        min-width: 34px;
        text-align: right;
    }

    .remaining {
        margin: 6px 0 0;
        font-size: 0.85rem;
        color: #a6e3a1;
    }

    .file {
        margin: 8px 0 0;
        font-size: 0.8rem;
        color: #a6adc8;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .temps {
        display: flex;
        flex-wrap: wrap;
        gap: 10px;
        margin-top: 10px;
        font-size: 0.78rem;
        color: #a6adc8;
    }

    .code-warning {
        background: #3b2f1e;
        border: 1px solid #fab387;
        border-radius: 8px;
        padding: 10px 12px;
        margin-bottom: 10px;
        font-size: 0.82rem;
        color: #f9e2af;
    }

    .code-warning p {
        margin: 6px 0 0;
        color: #d7c9a7;
        line-height: 1.35;
    }

    .code-form {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        margin-top: 8px;
    }

    .code-form input {
        flex: 1;
        min-width: 150px;
        background: #1e1e2e;
        border: 1px solid #45475a;
        border-radius: 6px;
        padding: 8px;
        color: #cdd6f4;
        font-size: 0.9rem;
    }

    .fix-code-btn,
    .save-code-btn {
        background: #fab387;
        color: #11111b;
        border: none;
        border-radius: 6px;
        padding: 8px 14px;
        font-weight: 600;
        cursor: pointer;
        margin-top: 8px;
        min-height: 40px;
    }

    .save-code-btn { margin-top: 0; }

    .save-code-btn:disabled {
        opacity: 0.6;
        cursor: not-allowed;
    }

    .cancel-code-btn {
        background: #313244;
        color: #cdd6f4;
        border: none;
        border-radius: 6px;
        padding: 8px 12px;
        cursor: pointer;
        min-height: 40px;
    }

    .code-hint {
        font-size: 0.75rem;
        color: #a6adc8;
        margin-top: 6px;
    }

    .stop-btn {
        margin-top: 12px;
        width: 100%;
        background: #f38ba8;
        color: #11111b;
        border: none;
        border-radius: 8px;
        padding: 10px;
        font-weight: 600;
        font-size: 0.9rem;
        cursor: pointer;
        min-height: 44px;
    }

    .stop-btn:hover { background: #eba0ac; }

    .stop-btn:disabled {
        opacity: 0.6;
        cursor: not-allowed;
    }

    .last-action {
        margin: 8px 0 0;
        font-size: 0.75rem;
        color: #6c7086;
    }

    .message.success {
        background: #2a3b2a;
        color: #a6e3a1;
        border: 1px solid #a6e3a1;
        padding: 10px 14px;
        border-radius: 8px;
        margin-bottom: 14px;
        font-size: 0.9rem;
    }

    .offline-note {
        margin: 8px 0 0;
        font-size: 0.82rem;
        color: #6c7086;
    }
</style>
