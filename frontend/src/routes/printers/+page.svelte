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
    // Sending a sliced file, so nobody has to switch wifi to print
    let sendFor = '';
    let printerFiles = {};
    let uploading = '';
    let uploadProgress = 0;
    let dragOver = '';

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

    function toggleSend(printer) {
        sendFor = sendFor === printer.id ? '' : printer.id;
        if (sendFor) loadPrinterFiles(printer);
    }

    async function loadPrinterFiles(printer) {
        try {
            const response = await fetch(`/api/printers/${printer.id}/files`);
            if (response.ok) {
                printerFiles = { ...printerFiles, [printer.id]: await response.json() };
            }
        } catch (e) {
            // The list is a convenience; the upload itself reports its own errors
        }
    }

    function handleDrop(printer, event) {
        dragOver = '';
        const file = event.dataTransfer?.files?.[0];
        if (file) uploadToPrinter(printer, file);
    }

    function handlePick(printer, event) {
        const file = event.target.files?.[0];
        if (file) uploadToPrinter(printer, file);
        event.target.value = '';
    }

    async function uploadToPrinter(printer, file) {
        if (!/\.(3mf|gcode)$/i.test(file.name)) {
            error = 'Only .3mf and .gcode files can be sent to a printer';
            setTimeout(() => (error = ''), 6000);
            return;
        }

        uploading = printer.id;
        uploadProgress = 0;

        const form = new FormData();
        form.append('file', file);

        const result = await new Promise((resolve) => {
            // XHR rather than fetch, because sliced files are big enough that
            // a progress bar is worth having
            const xhr = new XMLHttpRequest();
            xhr.open('POST', `/api/printers/${printer.id}/files`);
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

        uploading = '';
        uploadProgress = 0;

        if (result.status === 200) {
            notice = result.body.message || 'File sent to the printer';
            setTimeout(() => (notice = ''), 10000);
            loadPrinterFiles(printer);
        } else {
            error = result.body.error || 'The printer would not accept that file';
            setTimeout(() => (error = ''), 8000);
        }
    }

    function fileSize(bytes) {
        if (bytes > 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
        return `${Math.max(Math.round(bytes / 1024), 1)} KB`;
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
            {#each printers as printer, i (printer.id)}
                <div class="printer-card {stateClass(printer)}" style="--i: {i}">
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


                        <button class="send-toggle" on:click={() => toggleSend(printer)}>
                            {sendFor === printer.id ? '✕ Close' : '📤 Send a file to print'}
                        </button>

                        {#if sendFor === printer.id}
                            <div class="send-panel">
                                <p class="send-rule">
                                    <strong>Name your file with your name first</strong> -
                                    for example <code>srinath_bracket.3mf</code>. That is how
                                    everyone knows whose print it is.
                                </p>

                                <div
                                    class="dropzone"
                                    class:over={dragOver === printer.id}
                                    role="button"
                                    tabindex="0"
                                    on:dragover|preventDefault={() => (dragOver = printer.id)}
                                    on:dragleave={() => (dragOver = '')}
                                    on:drop|preventDefault={(e) => handleDrop(printer, e)}
                                    on:click={() => document.getElementById(`pick-${printer.id}`).click()}
                                    on:keydown={(e) => e.key === 'Enter' && document.getElementById(`pick-${printer.id}`).click()}
                                >
                                    <input
                                        id="pick-{printer.id}"
                                        type="file"
                                        accept=".3mf,.gcode"
                                        on:change={(e) => handlePick(printer, e)}
                                    />
                                    {#if uploading === printer.id}
                                        <span class="drop-title">Sending... {uploadProgress}%</span>
                                        <div class="upload-bar">
                                            <div class="upload-fill" style="width: {uploadProgress}%"></div>
                                        </div>
                                    {:else}
                                        <span class="drop-icon">📄</span>
                                        <span class="drop-title">Drop a sliced file here</span>
                                        <span class="drop-sub">or tap to choose - .3mf or .gcode</span>
                                    {/if}
                                </div>

                                <p class="send-note">
                                    Slice in Bambu Studio and export the file. It lands on the
                                    printer - then go to the machine, check the plate is clear,
                                    and start it from the screen.
                                </p>

                                {#if printerFiles[printer.id]}
                                    {#if printerFiles[printer.id].length === 0}
                                        <p class="send-empty">No files on this printer yet.</p>
                                    {:else}
                                        <p class="send-listhead">On this printer:</p>
                                        <ul class="send-list">
                                            {#each printerFiles[printer.id].slice(0, 8) as file}
                                                <li>
                                                    <span class="send-file" title={file.name}>{file.name}</span>
                                                    <span class="send-size">{fileSize(file.size)}</span>
                                                </li>
                                            {/each}
                                        </ul>
                                    {/if}
                                {/if}
                            </div>
                        {/if}


                                    {#if (printer.ams && printer.ams.length > 0) || printer.external_spool}
                                        <div class="ams">
                                            {#each printer.ams as unit}
                                                <div class="ams-head">
                                                    <span class="ams-title">AMS {unit.id + 1}</span>
                                                    {#if unit.humidity}
                                                        <span class="ams-meta" title="The AMS reports humidity on its own 1-5 scale">
                                                            💧 {unit.humidity}/5
                                                        </span>
                                                    {/if}
                                                    {#if unit.temp}
                                                        <span class="ams-meta">🌡️ {unit.temp}°C</span>
                                                    {/if}
                                                </div>

                                                <div class="ams-slots">
                                                    {#each unit.slots as slot}
                                                        <div
                                                            class="ams-slot"
                                                            class:empty={slot.empty}
                                                            class:active={slot.active}
                                                            title={slot.empty
                                                                ? `Slot ${slot.slot}: empty`
                                                                : `Slot ${slot.slot}: ${slot.material}${slot.remain >= 0 ? ', ' + slot.remain + '% left' : ', amount unknown'}${slot.active ? ' (in use)' : ''}`}
                                                        >
                                                            <span class="ams-top">
                                                                <span class="ams-swatch" style="background: {slot.color || 'transparent'}"></span>
                                                                <span class="ams-num">{slot.slot}</span>
                                                                {#if slot.active}<span class="ams-dot" title="Currently feeding"></span>{/if}
                                                            </span>

                                                            <span class="ams-mat">{slot.empty ? 'Empty' : slot.material}</span>

                                                            {#if slot.remain >= 0}
                                                                <span class="ams-bar">
                                                                    <span class="ams-fill" style="width: {slot.remain}%"></span>
                                                                </span>
                                                                <span class="ams-pct">{slot.remain}%</span>
                                                            {:else if !slot.empty}
                                                                <span class="ams-pct unknown" title="No RFID tag, so the printer cannot tell">? left</span>
                                                            {/if}
                                                        </div>
                                                    {/each}
                                                </div>
                                            {/each}

                                            {#if printer.external_spool && !printer.external_spool.empty}
                                                <div class="ams-ext">
                                                    <span class="ams-swatch" style="background: {printer.external_spool.color || 'transparent'}"></span>
                                                    <span class="ams-ext-label">External spool</span>
                                                    <span class="ams-ext-mat">{printer.external_spool.material}</span>
                                                    {#if printer.external_spool.remain >= 0}
                                                        <span class="ams-pct">{printer.external_spool.remain}%</span>
                                                    {/if}
                                                </div>
                                            {/if}
                                        </div>
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
        background: var(--ctp-base);
        color: var(--ctp-text);
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
        border-bottom: 1px solid var(--ctp-surface0);
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
        color: var(--ctp-peach);
    }

    .subtitle {
        margin: 2px 0 0;
        font-size: 0.85rem;
        color: var(--ctp-subtext0);
    }

    .back-link {
        color: var(--ctp-blue);
        text-decoration: none;
        font-size: 0.9rem;
    }

    .message.error {
        background: #3b2a2a;
        color: var(--ctp-red);
        border: 1px solid var(--ctp-red);
        padding: 10px 14px;
        border-radius: 8px;
        margin-bottom: 14px;
        font-size: 0.9rem;
    }

    .note,
    .footnote {
        color: var(--ctp-overlay0);
        font-size: 0.85rem;
        text-align: center;
        margin-top: 16px;
    }

    .printer-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 460px));
        justify-content: center;
        gap: 16px;
    }

    .printer-card {
        --accent: var(--ctp-overlay0);
        position: relative;
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
            box-shadow var(--normal) var(--ease),
            border-color var(--normal) var(--ease);
        animation: rise var(--slow) var(--ease) both;
        animation-delay: calc(var(--i, 0) * 70ms);
    }

    .printer-card:hover {
        transform: translateY(-2px);
        box-shadow: var(--shadow);
    }

    .printer-card.running { --accent: var(--ctp-green); }
    .printer-card.failed { --accent: var(--ctp-red); }
    .printer-card.paused { --accent: var(--ctp-yellow); }
    .printer-card.finished { --accent: var(--ctp-blue); }
    .printer-card.idle { --accent: var(--ctp-sapphire); }
    .printer-card.offline { opacity: 0.62; }

    /* A quiet heartbeat on the machines that are actually working */
    .printer-card.running .availability.busy::before {
        content: '';
        display: inline-block;
        width: 7px;
        height: 7px;
        margin-right: 6px;
        border-radius: 50%;
        background: var(--ctp-green);
        animation: soft-pulse 1.8s ease-in-out infinite;
    }

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

    .availability.free { color: var(--ctp-green); }
    .availability.busy { color: var(--ctp-peach); }

    .state-badge {
        font-size: 0.75rem;
        padding: 3px 9px;
        border-radius: 999px;
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        white-space: nowrap;
    }

    .state-badge.running { background: var(--ctp-green); color: var(--ctp-crust); }
    .state-badge.failed { background: var(--ctp-red); color: var(--ctp-crust); }
    .state-badge.paused { background: var(--ctp-yellow); color: var(--ctp-crust); }
    .state-badge.finished { background: var(--ctp-blue); color: var(--ctp-crust); }

    .camera {
        aspect-ratio: 16 / 9;
        background: var(--ctp-crust);
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius);
        overflow: hidden;
        margin-bottom: 12px;
    }

    .camera img {
        width: 100%;
        height: 100%;
        object-fit: cover;
        display: block;
        animation: fade var(--normal) var(--ease);
        transition: transform var(--slow) var(--ease);
    }

    .printer-card:hover .camera img {
        transform: scale(1.03);
    }

    .camera-placeholder {
        width: 100%;
        height: 100%;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        color: var(--ctp-surface1);
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
        background: var(--ctp-surface0);
        border-radius: 999px;
        overflow: hidden;
    }

    .progress-fill {
        position: relative;
        height: 100%;
        border-radius: var(--radius-pill);
        background: linear-gradient(90deg, var(--ctp-green), var(--ctp-teal));
        transition: width var(--slow) var(--ease);
        overflow: hidden;
    }

    /* Light sweeping across the bar, so it reads as live */
    .progress-fill::after {
        content: '';
        position: absolute;
        inset: 0;
        background: linear-gradient(
            90deg,
            transparent,
            rgba(205, 214, 244, 0.45),
            transparent
        );
        transform: translateX(-100%);
        animation: sweep 2.2s var(--ease) infinite;
    }

    @keyframes sweep {
        to {
            transform: translateX(100%);
        }
    }

    .progress-text {
        font-size: 0.8rem;
        color: var(--ctp-subtext0);
        min-width: 34px;
        text-align: right;
    }

    .remaining {
        margin: 6px 0 0;
        font-size: 0.85rem;
        color: var(--ctp-green);
    }

    .file {
        margin: 8px 0 0;
        font-size: 0.8rem;
        color: var(--ctp-subtext0);
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
        color: var(--ctp-subtext0);
    }

    .code-warning {
        background: #3b2f1e;
        border: 1px solid var(--ctp-peach);
        border-radius: 8px;
        padding: 10px 12px;
        margin-bottom: 10px;
        font-size: 0.82rem;
        color: var(--ctp-yellow);
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
        background: var(--ctp-base);
        border: 1px solid var(--ctp-surface1);
        border-radius: 6px;
        padding: 8px;
        color: var(--ctp-text);
        font-size: 0.9rem;
    }

    .fix-code-btn,
    .save-code-btn {
        background: var(--ctp-peach);
        color: var(--ctp-crust);
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
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        border: none;
        border-radius: 6px;
        padding: 8px 12px;
        cursor: pointer;
        min-height: 40px;
    }

    .code-hint {
        font-size: 0.75rem;
        color: var(--ctp-subtext0);
        margin-top: 6px;
    }

    .stop-btn {
        margin-top: 12px;
        width: 100%;
        background: var(--ctp-red);
        color: var(--ctp-crust);
        border: none;
        border-radius: 8px;
        padding: 10px;
        font-weight: 600;
        font-size: 0.9rem;
        cursor: pointer;
        min-height: 44px;
    }

    .stop-btn {
        transition:
            background-color var(--fast) var(--ease),
            transform var(--fast) var(--ease);
    }

    .stop-btn:hover { background: var(--ctp-maroon); }

    .stop-btn:active { transform: scale(0.985); }

    .stop-btn:disabled {
        opacity: 0.6;
        cursor: not-allowed;
    }

    .last-action {
        margin: 8px 0 0;
        font-size: 0.75rem;
        color: var(--ctp-overlay0);
    }

    .message.success {
        background: #2a3b2a;
        color: var(--ctp-green);
        border: 1px solid var(--ctp-green);
        padding: 10px 14px;
        border-radius: 8px;
        margin-bottom: 14px;
        font-size: 0.9rem;
    }




    /* --- AMS ------------------------------------------------------------ */
    .ams {
        margin-top: 12px;
        padding: 10px;
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-sm);
        background: var(--ctp-crust);
    }

    .ams-head {
        display: flex;
        align-items: center;
        gap: 10px;
        flex-wrap: wrap;
        margin-bottom: 8px;
    }

    .ams-title {
        font-size: 0.72rem;
        font-weight: 700;
        letter-spacing: 0.6px;
        text-transform: uppercase;
        color: var(--ctp-teal);
    }

    .ams-meta {
        font-size: 0.7rem;
        color: var(--ctp-subtext0);
    }

    .ams-slots {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(72px, 1fr));
        gap: 6px;
    }

    .ams-slot {
        display: flex;
        flex-direction: column;
        gap: 4px;
        padding: 7px;
        border: 1px solid var(--ctp-surface1);
        border-radius: var(--radius-sm);
        background: var(--ctp-mantle);
        min-width: 0;
    }

    .ams-slot.active {
        border-color: var(--ctp-green);
        box-shadow: 0 0 0 1px var(--ctp-green) inset;
    }

    .ams-slot.empty { opacity: 0.45; }

    .ams-top {
        display: flex;
        align-items: center;
        gap: 5px;
    }

    .ams-swatch {
        width: 14px;
        height: 14px;
        border-radius: 50%;
        border: 1px solid var(--ctp-surface2);
        flex-shrink: 0;
    }

    .ams-num {
        font-size: 0.65rem;
        color: var(--ctp-overlay0);
        font-weight: 700;
    }

    .ams-dot {
        width: 6px;
        height: 6px;
        border-radius: 50%;
        background: var(--ctp-green);
        margin-left: auto;
        animation: soft-pulse 1.8s ease-in-out infinite;
    }

    .ams-mat {
        font-size: 0.68rem;
        color: var(--ctp-text);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .ams-bar {
        display: block;
        height: 4px;
        background: var(--ctp-surface0);
        border-radius: var(--radius-pill);
        overflow: hidden;
    }

    .ams-fill {
        display: block;
        height: 100%;
        background: linear-gradient(90deg, var(--ctp-teal), var(--ctp-sky));
        transition: width var(--slow) var(--ease);
    }

    .ams-pct {
        font-size: 0.62rem;
        color: var(--ctp-subtext0);
    }

    .ams-pct.unknown { color: var(--ctp-overlay0); }

    .ams-ext {
        display: flex;
        align-items: center;
        gap: 7px;
        margin-top: 8px;
        padding-top: 8px;
        border-top: 1px dashed var(--ctp-surface1);
        font-size: 0.7rem;
    }

    .ams-ext-label {
        color: var(--ctp-overlay0);
        text-transform: uppercase;
        letter-spacing: 0.4px;
        font-size: 0.62rem;
    }

    .ams-ext-mat {
        color: var(--ctp-text);
        flex: 1;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .send-toggle {
        margin-top: 12px;
        width: 100%;
        min-height: 44px;
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        border: 1px solid var(--ctp-surface1);
        border-radius: var(--radius-sm);
        font-weight: 600;
        font-family: inherit;
        font-size: 0.88rem;
        cursor: pointer;
        transition: background-color var(--fast) var(--ease), transform var(--fast) var(--ease);
    }

    .send-toggle:hover {
        background: var(--ctp-surface1);
        transform: translateY(-1px);
    }

    .send-panel {
        margin-top: 10px;
        padding: 12px;
        border: 1px solid var(--ctp-surface0);
        border-radius: var(--radius-sm);
        background: var(--ctp-crust);
        animation: rise var(--normal) var(--ease) both;
    }

    .send-rule {
        margin: 0 0 10px;
        font-size: 0.78rem;
        line-height: 1.45;
        color: var(--ctp-subtext0);
    }

    .send-rule strong { color: var(--ctp-mauve); }

    .send-rule code {
        background: var(--ctp-mantle);
        border: 1px solid var(--ctp-surface0);
        border-radius: 4px;
        padding: 1px 5px;
        color: var(--ctp-text);
    }

    .dropzone {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 4px;
        padding: 20px 12px;
        border: 2px dashed var(--ctp-surface2);
        border-radius: var(--radius);
        cursor: pointer;
        text-align: center;
        transition:
            border-color var(--fast) var(--ease),
            background-color var(--fast) var(--ease),
            transform var(--fast) var(--ease);
    }

    .dropzone:hover {
        border-color: var(--ctp-mauve);
        background: rgba(203, 166, 247, 0.06);
    }

    .dropzone.over {
        border-color: var(--ctp-mauve);
        background: rgba(203, 166, 247, 0.14);
        transform: scale(1.01);
    }

    .dropzone input { display: none; }

    .drop-icon { font-size: 1.6rem; }

    .drop-title {
        font-size: 0.88rem;
        font-weight: 600;
        color: var(--ctp-text);
    }

    .drop-sub {
        font-size: 0.75rem;
        color: var(--ctp-overlay0);
    }

    .upload-bar {
        width: 100%;
        height: 6px;
        background: var(--ctp-surface0);
        border-radius: var(--radius-pill);
        overflow: hidden;
        margin-top: 8px;
    }

    .upload-fill {
        height: 100%;
        background: linear-gradient(90deg, var(--ctp-mauve), var(--ctp-lavender));
        transition: width var(--fast) linear;
    }

    .send-note {
        margin: 10px 0 0;
        font-size: 0.73rem;
        line-height: 1.45;
        color: var(--ctp-overlay0);
    }

    .send-listhead {
        margin: 12px 0 4px;
        font-size: 0.72rem;
        text-transform: uppercase;
        letter-spacing: 0.4px;
        color: var(--ctp-overlay0);
    }

    .send-empty {
        margin: 10px 0 0;
        font-size: 0.75rem;
        color: var(--ctp-overlay0);
    }

    .send-list {
        list-style: none;
        margin: 0;
        padding: 0;
        display: flex;
        flex-direction: column;
        gap: 3px;
    }

    .send-list li {
        display: flex;
        gap: 8px;
        font-size: 0.72rem;
        background: var(--ctp-mantle);
        border-radius: 5px;
        padding: 5px 8px;
    }

    .send-file {
        flex: 1;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .send-size { color: var(--ctp-overlay0); }

    .offline-note {
        margin: 8px 0 0;
        font-size: 0.82rem;
        color: var(--ctp-overlay0);
    }
</style>
