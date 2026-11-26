<script>
    import { onMount, onDestroy } from 'svelte';
    import { cameraFeed } from '$lib/camera.js';
    import { api, can, getToken, getUser, loginUrl, refreshUser } from '$lib/auth.js';

    const STATUS_INTERVAL = 5000;   // printers report every few seconds
    const CAMERA_INTERVAL = 2000;   // the camera itself only manages ~0.5 fps
    const FILES_SHOWN = 8;          // longer lists fold behind "Show all"

    let printers = [];
    let loaded = false;
    let error = '';
    let notice = '';
    // The logged-in user (with permissions), for the access-code fix, sending
    // files and tidying files off a printer
    let user = null;
    let loginHref = '/login';
    // Printer id currently showing the "change access code" box
    let editingCode = '';
    let newCode = '';
    let savingCode = false;
    // Sending a sliced file, so nobody has to switch wifi to print
    let sendFor = '';
    let printerFiles = {};
    let filesLoading = {};
    let filesError = {};
    let showAllFiles = {};
    let deleting = '';          // "<printer id>/<file name>", or "<printer id>/*" for clear all
    let uploading = '';
    let uploadProgress = 0;
    let dragOver = '';

    // Latest good frame per printer
    const camera = cameraFeed();
    let statusTimer;
    let cameraTimer;
    let pendingTimers = [];

    $: canUpload = can(user, 'upload_prints');
    $: canManage = can(user, 'manage_printers');

    // At-a-glance counts for the strip under the title
    $: summary = {
        free: printers.filter((p) => p.online && !isPrinting(p) && !['PAUSE', 'FAILED'].includes(p.state)).length,
        printing: printers.filter(isPrinting).length,
        attention: printers.filter((p) => p.online && ['PAUSE', 'FAILED'].includes(p.state)).length,
        offline: printers.filter((p) => !p.online).length
    };

    onMount(() => {
        user = getUser();
        loginHref = loginUrl();
        // Permissions may have changed since login; a 401 ends the session
        refreshUser().then((fresh) => (user = fresh));

        loadPrinters();
        statusTimer = setInterval(loadPrinters, STATUS_INTERVAL);
        cameraTimer = setInterval(() => camera.refresh(printers), CAMERA_INTERVAL);
    });

    onDestroy(() => {
        clearInterval(statusTimer);
        clearInterval(cameraTimer);
        pendingTimers.forEach(clearTimeout);
        camera.destroy();
    });

    // setTimeout that is cancelled if the page is left first
    function later(fn, ms) {
        pendingTimers.push(setTimeout(fn, ms));
    }

    function showError(message, ms = 8000) {
        error = message;
        later(() => {
            if (error === message) error = '';
        }, ms);
    }

    function showNotice(message, ms = 10000) {
        notice = message;
        later(() => {
            if (notice === message) notice = '';
        }, ms);
    }

    async function loadPrinters() {
        try {
            const response = await fetch('/api/printers');
            if (response.ok) {
                printers = await response.json();
                if (error === 'Could not load printer status' || error === 'Could not reach the server') error = '';
            } else {
                error = 'Could not load printer status';
            }
        } catch (e) {
            error = 'Could not reach the server';
        } finally {
            loaded = true;
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
            const response = await api(`/api/admin/printers/${printer.id}/access-code`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ access_code: newCode.trim() })
            });

            if (response.status === 401) {
                user = null;
                error = 'Your session expired. Log in again.';
                return;
            }

            // {} when the body is not JSON, e.g. a 502 page from the proxy
            const result = await response.json().catch(() => ({}));
            if (response.ok) {
                showNotice(`${printer.name}: access code updated, reconnecting...`, 8000);
                editingCode = '';
                newCode = '';
                // Reconnection takes a few seconds
                later(loadPrinters, 4000);
                later(loadPrinters, 12000);
            } else {
                error = result.error || `Could not update the access code (HTTP ${response.status})`;
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

    // Public: anyone can see what is on a printer
    async function loadPrinterFiles(printer) {
        filesLoading = { ...filesLoading, [printer.id]: true };
        try {
            const response = await fetch(`/api/printers/${printer.id}/files`);
            const body = await response.json().catch(() => null);
            if (response.ok && Array.isArray(body)) {
                printerFiles = { ...printerFiles, [printer.id]: body };
                filesError = { ...filesError, [printer.id]: '' };
            } else {
                filesError = {
                    ...filesError,
                    [printer.id]: body?.error || `Could not read the files on this printer (HTTP ${response.status})`
                };
            }
        } catch (e) {
            filesError = { ...filesError, [printer.id]: 'Could not reach the server' };
        } finally {
            filesLoading = { ...filesLoading, [printer.id]: false };
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

    function pickFile(printer) {
        if (uploading) return;
        document.getElementById(`pick-${printer.id}`)?.click();
    }

    async function uploadToPrinter(printer, file) {
        if (uploading) return;
        if (!/\.(3mf|gcode)$/i.test(file.name)) {
            showError('Only .3mf and .gcode files can be sent to a printer', 6000);
            return;
        }
        if (!can(user, 'upload_prints')) {
            if (!user) {
                location.href = loginUrl();
                return;
            }
            showError("Your account can't send files to printers. Ask a server admin.");
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
            xhr.setRequestHeader('Authorization', `Bearer ${getToken()}`);
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
            showNotice(result.body.message || 'File sent to the printer');
            loadPrinterFiles(printer);
        } else if (result.status === 401) {
            // The session ended while the page was open
            user = null;
            showError('Your session expired. Log in again to send files.');
        } else if (result.body.error) {
            showError(result.body.error);
        } else {
            // The proxy's own errors (size limit, backend down) have no JSON body
            if (result.status === 413) {
                showError('That file is too large to send to the printer');
            } else if (result.status === 429) {
                showError('Too many uploads. Wait a few minutes and try again');
            } else if (result.status === 0) {
                showError('The upload was interrupted. Check your connection and try again');
            } else if (result.status >= 502 && result.status <= 504) {
                showError('The server is not responding right now. Try again in a minute');
            } else {
                showError(`The printer would not accept that file (HTTP ${result.status})`);
            }
        }
    }

    // Tidying files off a printer is for people who manage the printers
    async function deleteFile(printer, file) {
        if (!confirm(`Delete ${file.name} from ${printer.name}? This cannot be undone.`)) return;
        await adminDelete(
            printer,
            `${printer.id}/${file.name}`,
            `/api/admin/printers/${printer.id}/files/${encodeURIComponent(file.name)}`,
            `${file.name} deleted from ${printer.name}`
        );
    }

    async function clearAllFiles(printer) {
        const count = printerFiles[printer.id]?.length || 0;
        if (!confirm(`Delete all ${count} file(s) from ${printer.name}? This cannot be undone.`)) return;
        await adminDelete(printer, `${printer.id}/*`, `/api/admin/printers/${printer.id}/files`, null);
    }

    async function adminDelete(printer, key, path, okMessage) {
        deleting = key;
        try {
            const response = await api(path, { method: 'DELETE' });
            if (response.status === 401) {
                user = null;
                showError('Your session expired. Log in again.');
                return;
            }
            const result = await response.json().catch(() => ({}));
            if (response.ok) {
                showNotice(okMessage || result.message || 'Deleted');
            } else if (response.status === 403) {
                showError("Your account can't delete printer files. Ask a server admin.");
            } else {
                showError(result.error || `Could not delete (HTTP ${response.status})`);
            }
        } catch (e) {
            showError('Could not reach the server');
        } finally {
            deleting = '';
            loadPrinterFiles(printer);
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

    // Wall-clock finish time, which is what people plan around
    function finishTime(minutes) {
        if (!minutes || minutes <= 0) return '';
        const done = new Date(Date.now() + minutes * 60000);
        const sameDay = done.toDateString() === new Date().toDateString();
        const time = done.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        return sameDay ? `done ~${time}` : `done ~${done.toLocaleDateString([], { weekday: 'short' })} ${time}`;
    }

    // Slicers add a lot of noise to file names
    function tidyFileName(name) {
        if (!name) return '';
        return name.replace(/\.gcode\.3mf$/i, '').replace(/\.3mf$/i, '');
    }

    // The file the printer is working from cannot be deleted mid-job
    function isCurrentJob(printer, file) {
        return isPrinting(printer) && tidyFileName(file.name) === tidyFileName(printer.file_name);
    }
</script>

<div class="container">
    <div class="header">
        <div class="logo-title">
            <h1>🖨️ 3D Printers</h1>
            <p class="subtitle">Live status of the lab's Bambu Lab P1S printers</p>
        </div>
        <a href="/?view=printer-guide" class="guide-link">📖 How to print</a>
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
        <div class="summary" aria-label="Printer summary">
            <span class="pill free"><span class="pip"></span>{summary.free} free</span>
            <span class="pill printing"><span class="pip"></span>{summary.printing} printing</span>
            {#if summary.attention}
                <span class="pill attention"><span class="pip"></span>{summary.attention} paused or failed</span>
            {/if}
            {#if summary.offline}
                <span class="pill offline"><span class="pip"></span>{summary.offline} offline</span>
            {/if}
        </div>

        <div class="printer-grid">
            {#each printers as printer, i (printer.id)}
                <div class="printer-card {stateClass(printer)}" style="--i: {i}">
                    <div class="card-head">
                        <img src="/P1S.png" alt="" class="printer-icon" />
                        <div class="head-text">
                            <h2>{printer.name}</h2>
                            <span class="availability {isPrinting(printer) ? 'busy' : printer.online ? 'free' : 'unknown'}">
                                {availability(printer)}
                            </span>
                        </div>
                        <span class="state-badge {stateClass(printer)}">{stateLabel(printer)}</span>
                    </div>

                    {#if printer.access_code_problem}
                        <div class="code-warning">
                            <strong>⚠️ Camera not connecting</strong>
                            <p>
                                {printer.name} accepted the connection and then hung up. Usually
                                the access code changed - toggling LAN mode on the printer
                                regenerates it. It also happens when the camera is already open
                                in Bambu Studio or Handy, since the P1S allows only one viewer.
                            </p>
                            {#if canManage}
                                {#if editingCode === printer.id}
                                    <div class="code-form">
                                        <input
                                            type="text"
                                            bind:value={newCode}
                                            placeholder="New access code from the printer screen"
                                            autocomplete="off"
                                            on:keydown={(e) => e.key === 'Enter' && saveAccessCode(printer)}
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
                                <p class="code-hint">A printer admin can fix this from this page.</p>
                            {/if}
                        </div>
                    {/if}

                    <div class="camera">
                        {#if printer.camera_online && $camera[printer.id]}
                            <img
                                src={$camera[printer.id]}
                                alt="Camera view of {printer.name}"
                            />
                        {:else}
                            <div class="camera-placeholder">
                                <img src="/P1S.png" alt="" class="placeholder-icon" />
                                <p>{printer.camera_online ? 'Loading camera…' : 'No camera image'}</p>
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
                                <p class="remaining">
                                    {formatRemaining(printer.remaining_minutes)}
                                    <span class="eta">· {finishTime(printer.remaining_minutes)}</span>
                                </p>
                            {/if}
                        {/if}

                        {#if printer.file_name}
                            <p class="file" title={printer.file_name}>
                                📄 {tidyFileName(printer.file_name)}
                            </p>
                        {/if}

                        <button
                            class="send-toggle"
                            aria-expanded={sendFor === printer.id}
                            on:click={() => toggleSend(printer)}
                        >
                            {#if sendFor === printer.id}
                                ✕ Close
                            {:else if canUpload}
                                📤 Send a file to print
                            {:else}
                                📂 Files on this printer
                            {/if}
                        </button>

                        {#if sendFor === printer.id}
                            <div class="send-panel">
                                {#if canUpload}
                                    <p class="send-rule">
                                        <strong>Name your file with your name first</strong> -
                                        for example <code>srinath_bracket.3mf</code>. That is how
                                        everyone knows whose print it is.
                                    </p>

                                    <div
                                        class="dropzone"
                                        class:over={dragOver === printer.id}
                                        class:busy={uploading === printer.id}
                                        role="button"
                                        tabindex="0"
                                        aria-label="Send a sliced file to {printer.name}"
                                        on:dragover|preventDefault={() => (dragOver = printer.id)}
                                        on:dragleave={() => (dragOver = '')}
                                        on:drop|preventDefault={(e) => handleDrop(printer, e)}
                                        on:click={() => pickFile(printer)}
                                        on:keydown={(e) => (e.key === 'Enter' || e.key === ' ') && (e.preventDefault(), pickFile(printer))}
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
                                {:else if !user}
                                    <div class="send-locked">
                                        <p>Log in to send a sliced file to {printer.name} from here.</p>
                                        <a class="login-btn" href={loginHref}>🔑 Log in to send files</a>
                                    </div>
                                {:else}
                                    <div class="send-locked">
                                        <p>
                                            Your account can't send files to printers yet. Ask a
                                            server admin for the upload permission.
                                        </p>
                                    </div>
                                {/if}

                                <div class="files-head">
                                    <p class="send-listhead">
                                        On this printer{printerFiles[printer.id] ? ` (${printerFiles[printer.id].length})` : ''}
                                    </p>
                                    <div class="files-actions">
                                        <button
                                            class="mini-btn"
                                            on:click={() => loadPrinterFiles(printer)}
                                            disabled={filesLoading[printer.id]}
                                            title="Read the file list again"
                                        >
                                            {filesLoading[printer.id] ? '...' : '↻ Refresh'}
                                        </button>
                                        {#if canManage && printerFiles[printer.id]?.length > 0}
                                            <button
                                                class="mini-btn danger"
                                                on:click={() => clearAllFiles(printer)}
                                                disabled={!!deleting || isPrinting(printer)}
                                                title={isPrinting(printer) ? 'Wait for the job to finish' : 'Delete every file on this printer'}
                                            >
                                                {deleting === `${printer.id}/*` ? 'Clearing...' : '🗑️ Clear all'}
                                            </button>
                                        {/if}
                                    </div>
                                </div>

                                {#if filesError[printer.id]}
                                    <p class="send-error">{filesError[printer.id]}</p>
                                {/if}

                                {#if printerFiles[printer.id]}
                                    {#if printerFiles[printer.id].length === 0}
                                        <p class="send-empty">No files on this printer yet.</p>
                                    {:else}
                                        <ul class="send-list">
                                            {#each showAllFiles[printer.id] ? printerFiles[printer.id] : printerFiles[printer.id].slice(0, FILES_SHOWN) as file (file.name)}
                                                <li>
                                                    <span class="send-info">
                                                        <span class="send-file" title={file.name}>{file.name}</span>
                                                        <span class="send-meta">
                                                            {fileSize(file.size)}{file.time ? ` · ${file.time}` : ''}
                                                            {#if isCurrentJob(printer, file)}<span class="send-now"> · printing</span>{/if}
                                                        </span>
                                                    </span>
                                                    {#if canManage}
                                                        <button
                                                            class="delete-btn"
                                                            on:click={() => deleteFile(printer, file)}
                                                            disabled={!!deleting || isCurrentJob(printer, file)}
                                                            title={isCurrentJob(printer, file) ? 'This file is printing right now' : `Delete ${file.name}`}
                                                            aria-label="Delete {file.name}"
                                                        >
                                                            {deleting === `${printer.id}/${file.name}` ? '...' : '🗑️'}
                                                        </button>
                                                    {/if}
                                                </li>
                                            {/each}
                                        </ul>
                                        {#if printerFiles[printer.id].length > FILES_SHOWN}
                                            <button
                                                class="show-all"
                                                on:click={() => (showAllFiles = { ...showAllFiles, [printer.id]: !showAllFiles[printer.id] })}
                                            >
                                                {showAllFiles[printer.id]
                                                    ? 'Show fewer'
                                                    : `Show all ${printerFiles[printer.id].length} files`}
                                            </button>
                                        {/if}
                                    {/if}
                                {:else if filesLoading[printer.id]}
                                    <p class="send-empty">Reading the printer's files...</p>
                                {/if}
                            </div>
                        {/if}

                        {#if (printer.ams && printer.ams.length > 0) || printer.external_spool}
                            <div class="ams">
                                {#each printer.ams || [] as unit}
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
                        {#if printer.last_action_by}
                            <p class="last-action">Last stopped by {printer.last_action_by}</p>
                        {/if}
                    {:else}
                        <p class="offline-note">
                            Not responding - it may be switched off or off the network.
                            This page keeps checking and updates by itself when it is back.
                        </p>
                    {/if}
                </div>
            {/each}
        </div>

        <p class="footnote">
            Status refreshes automatically. The camera updates about once every two
            seconds - that is the fastest the P1S allows over the local network.
        </p>
    {/if}
</div>

<style>
    .container {
        max-width: 1200px;
        margin: 0 auto;
        padding: 16px clamp(16px, 3vw, 28px) 32px;
        box-sizing: border-box;
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

    .guide-link {
        display: inline-flex;
        align-items: center;
        min-height: 40px;
        color: var(--link);
        text-decoration: none;
        font-size: 0.9rem;
    }

    .guide-link:hover {
        text-decoration: underline;
    }

    /* The shell floats .message as a toast; keep the old tinted look */
    .message.error {
        background: color-mix(in srgb, var(--ctp-red) 14%, var(--card));
    }

    .message.success {
        background: color-mix(in srgb, var(--ctp-green) 14%, var(--card));
    }

    .note,
    .footnote {
        color: var(--ctp-overlay0);
        font-size: 0.85rem;
        text-align: center;
        margin-top: 16px;
    }

    .summary {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-bottom: 16px;
    }

    .pill {
        --pip: var(--ctp-overlay0);
        display: inline-flex;
        align-items: center;
        gap: 7px;
        padding: 5px 12px;
        border-radius: var(--radius-pill);
        border: 1px solid var(--ctp-surface0);
        background: var(--ctp-mantle);
        font-size: 0.8rem;
        font-weight: 600;
        color: var(--ctp-subtext0);
    }

    .pill .pip {
        width: 8px;
        height: 8px;
        border-radius: 50%;
        background: var(--pip);
    }

    .pill.free { --pip: var(--ctp-green); }
    .pill.printing { --pip: var(--ctp-peach); }
    .pill.attention { --pip: var(--ctp-red); }

    .printer-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 460px));
        justify-content: center;
        align-items: start;
        gap: 16px;
    }

    .printer-card {
        --stripe: var(--ctp-overlay0);
        position: relative;
        min-width: 0;
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--ctp-text) 3.5%, transparent), transparent 55%),
            var(--ctp-mantle);
        border: 1px solid var(--ctp-surface0);
        border-left: 4px solid var(--stripe);
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

    /* No staggered entrance for people who asked for less motion */
    @media (prefers-reduced-motion: reduce) {
        .printer-card { animation-delay: 0s; }
    }

    .printer-card:hover {
        transform: translateY(-2px);
        box-shadow: var(--shadow);
    }

    .printer-card.running { --stripe: var(--ctp-green); }
    .printer-card.failed { --stripe: var(--ctp-red); }
    .printer-card.paused { --stripe: var(--ctp-yellow); }
    .printer-card.finished { --stripe: var(--ctp-blue); }
    .printer-card.idle { --stripe: var(--ctp-sapphire); }

    /* Faded, but the access-code fix stays fully readable */
    .printer-card.offline > :not(.code-warning) { opacity: 0.62; }

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
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .availability {
        font-size: 0.8rem;
        font-weight: 600;
    }

    .availability.free { color: var(--ctp-green); }
    .availability.busy { color: var(--ctp-peach); }
    .availability.unknown { color: var(--ctp-overlay0); }

    .state-badge {
        font-size: 0.75rem;
        padding: 3px 9px;
        border-radius: 999px;
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        white-space: nowrap;
    }

    .state-badge.running { background: var(--ctp-green); color: var(--rp-base); }
    .state-badge.failed { background: var(--ctp-red); color: var(--rp-base); }
    .state-badge.paused { background: var(--ctp-yellow); color: var(--rp-base); }
    .state-badge.finished { background: var(--ctp-blue); color: var(--rp-base); }

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
        color: var(--ctp-overlay0);
    }

    /* Scoped past the ".camera img" rule above, which would stretch it */
    .camera-placeholder .placeholder-icon {
        width: 64px;
        height: 64px;
        object-fit: contain;
        opacity: 0.35;
    }

    .printer-card:hover .camera-placeholder .placeholder-icon {
        transform: none;
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
        background: linear-gradient(90deg, var(--ctp-green), var(--ctp-blue));
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
            color-mix(in srgb, var(--rp-surface) 45%, transparent),
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

    .eta { color: var(--ctp-subtext0); }

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
        background: color-mix(in srgb, var(--ctp-peach) 12%, var(--card));
        border: 1px solid var(--ctp-peach);
        border-radius: 8px;
        padding: 10px 12px;
        margin-bottom: 10px;
        font-size: 0.82rem;
        color: var(--ctp-yellow);
    }

    .code-warning p {
        margin: 6px 0 0;
        color: var(--ctp-text);
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
        min-height: 40px;
        box-sizing: border-box;
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
        color: var(--rp-base);
        border: none;
        border-radius: 6px;
        padding: 8px 14px;
        font-weight: 600;
        font-family: inherit;
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
        font-family: inherit;
        cursor: pointer;
        min-height: 40px;
    }

    .code-hint {
        font-size: 0.75rem;
        color: var(--ctp-subtext0);
        margin-top: 6px;
    }

    .last-action {
        margin: 8px 0 0;
        font-size: 0.75rem;
        color: var(--ctp-overlay0);
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
        grid-template-columns: repeat(auto-fit, minmax(60px, 1fr));
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
        background: linear-gradient(90deg, var(--ctp-blue), var(--ctp-teal));
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

    /* No AMS units above it: no divider needed */
    .ams-ext:first-child {
        margin-top: 0;
        padding-top: 0;
        border-top: none;
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

    /* --- sending files and the file list --------------------------------- */
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
        background: color-mix(in srgb, var(--ctp-mauve) 6%, transparent);
    }

    .dropzone.over {
        border-color: var(--ctp-mauve);
        background: color-mix(in srgb, var(--ctp-mauve) 14%, transparent);
        transform: scale(1.01);
    }

    .dropzone.busy { cursor: progress; }

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
        background: linear-gradient(90deg, var(--ctp-mauve), var(--ctp-rosewater));
        transition: width var(--fast) linear;
    }

    .send-note {
        margin: 10px 0 0;
        font-size: 0.73rem;
        line-height: 1.45;
        color: var(--ctp-overlay0);
    }

    .send-locked {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 8px;
        padding: 14px 12px;
        border: 2px dashed var(--ctp-surface1);
        border-radius: var(--radius);
        text-align: center;
    }

    .send-locked p {
        margin: 0;
        font-size: 0.8rem;
        line-height: 1.45;
        color: var(--ctp-subtext0);
    }

    .login-btn {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        min-height: 40px;
        padding: 0 16px;
        border-radius: var(--radius-sm);
        background: var(--accent);
        color: var(--accent-ink);
        font-weight: 600;
        font-size: 0.88rem;
        text-decoration: none;
    }

    .login-btn:hover { filter: brightness(1.05); }

    .files-head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 8px;
        flex-wrap: wrap;
        margin: 14px 0 6px;
        padding-top: 10px;
        border-top: 1px solid var(--ctp-surface0);
    }

    .send-listhead {
        margin: 0;
        font-size: 0.72rem;
        font-weight: 700;
        text-transform: uppercase;
        letter-spacing: 0.4px;
        color: var(--ctp-overlay0);
    }

    .files-actions {
        display: flex;
        gap: 6px;
    }

    .mini-btn {
        min-height: 40px;
        padding: 0 12px;
        background: var(--ctp-surface0);
        color: var(--ctp-text);
        border: 1px solid var(--ctp-surface1);
        border-radius: 6px;
        font-family: inherit;
        font-size: 0.78rem;
        font-weight: 600;
        cursor: pointer;
    }

    .mini-btn:hover:not(:disabled) { background: var(--ctp-surface1); }

    .mini-btn.danger { color: var(--danger); }

    .mini-btn:disabled,
    .delete-btn:disabled {
        opacity: 0.5;
        cursor: not-allowed;
    }

    .send-empty,
    .send-error {
        margin: 6px 0 0;
        font-size: 0.75rem;
        color: var(--ctp-overlay0);
    }

    .send-error { color: var(--danger); }

    .send-list {
        list-style: none;
        margin: 0;
        padding: 0;
        display: flex;
        flex-direction: column;
        gap: 4px;
    }

    .send-list li {
        display: flex;
        align-items: center;
        gap: 8px;
        font-size: 0.75rem;
        background: var(--ctp-mantle);
        border: 1px solid var(--ctp-surface0);
        border-radius: 6px;
        padding: 6px 6px 6px 10px;
        min-height: 40px;
        box-sizing: border-box;
    }

    .send-info {
        flex: 1;
        min-width: 0;
        display: flex;
        flex-direction: column;
        gap: 1px;
    }

    .send-file {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        color: var(--ctp-text);
    }

    .send-meta {
        font-size: 0.68rem;
        color: var(--ctp-overlay0);
    }

    .send-now { color: var(--ctp-green); }

    .delete-btn {
        flex-shrink: 0;
        width: 40px;
        height: 40px;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        background: transparent;
        border: 1px solid transparent;
        border-radius: 6px;
        color: var(--danger);
        font-size: 0.95rem;
        cursor: pointer;
        transition: background-color var(--fast) var(--ease), border-color var(--fast) var(--ease);
    }

    .delete-btn:hover:not(:disabled) {
        background: color-mix(in srgb, var(--danger) 12%, transparent);
        border-color: color-mix(in srgb, var(--danger) 40%, transparent);
    }

    .show-all {
        margin-top: 6px;
        width: 100%;
        min-height: 40px;
        background: transparent;
        border: 1px dashed var(--ctp-surface1);
        border-radius: 6px;
        color: var(--link);
        font-family: inherit;
        font-size: 0.78rem;
        cursor: pointer;
    }

    .offline-note {
        margin: 8px 0 0;
        font-size: 0.82rem;
        color: var(--ctp-overlay0);
    }
</style>
