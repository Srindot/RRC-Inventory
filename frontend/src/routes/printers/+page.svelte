<script>
    import { onMount, onDestroy } from 'svelte';
    import { cameraFeed } from '$lib/camera.js';
    import { api, can, getToken, getUser, loginUrl } from '$lib/auth.js';
    import Icon from '$lib/Icon.svelte';
    import { AREA } from '$lib/palette.js';

    const STATUS_INTERVAL = 5000;   // printers report every few seconds
    const CAMERA_INTERVAL = 2000;   // the camera itself only manages ~0.5 fps

    let printers = [];
    let loaded = false;
    let error = '';
    let notice = '';
    // Admin actions live on the admin page; this page only needs the user to
    // offer the access-code fix and to send files.
    let user = null;
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

    // Latest good frame per printer
    const camera = cameraFeed();
    let statusTimer;
    let cameraTimer;

    onMount(() => {
        user = getUser();

        loadPrinters();
        statusTimer = setInterval(loadPrinters, STATUS_INTERVAL);
        cameraTimer = setInterval(() => camera.refresh(printers), CAMERA_INTERVAL);
    });

    onDestroy(() => {
        clearInterval(statusTimer);
        clearInterval(cameraTimer);
        camera.destroy();
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
                notice = `${printer.name}: access code updated, reconnecting...`;
                setTimeout(() => (notice = ''), 8000);
                editingCode = '';
                newCode = '';
                // Reconnection takes a few seconds
                setTimeout(loadPrinters, 4000);
                setTimeout(loadPrinters, 12000);
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
        if (!can(user, 'upload_prints')) {
            if (!user) {
                location.href = loginUrl();
                return;
            }
            error = "Your account can't send files to printers. Ask a server admin.";
            setTimeout(() => (error = ''), 8000);
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
            notice = result.body.message || 'File sent to the printer';
            setTimeout(() => (notice = ''), 10000);
            loadPrinterFiles(printer);
        } else if (result.body.error) {
            error = result.body.error;
            setTimeout(() => (error = ''), 8000);
        } else {
            // The proxy's own errors (size limit, backend down) have no JSON body
            if (result.status === 413) {
                error = 'That file is too large to send to the printer';
            } else if (result.status === 429) {
                error = 'Too many uploads. Wait a few minutes and try again';
            } else if (result.status === 0) {
                error = 'The upload was interrupted. Check your connection and try again';
            } else if (result.status >= 502 && result.status <= 504) {
                error = 'The server is not responding right now. Try again in a minute';
            } else {
                error = `The printer would not accept that file (HTTP ${result.status})`;
            }
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

    // Green printing, blue ready, yellow paused; problems keep the semantic colours
    function chip(printer) {
        if (!printer.online) return { cls: 'warn', hue: '' };
        if (printer.state === 'FAILED') return { cls: 'danger', hue: '' };
        if (printer.state === 'PAUSE') return { cls: 'hue', hue: 'var(--ctp-yellow)' };
        return { cls: 'hue', hue: isPrinting(printer) ? 'var(--ctp-green)' : 'var(--ctp-blue)' };
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

<div class="page">
    <header class="page-head">
        <div class="title">
            <span class="icon-badge" style="--hue: {AREA.printers}"><Icon name="printer" /></span>
            <div>
                <h1>3D printers</h1>
                <p>Live status of the lab's Bambu Lab P1S printers</p>
            </div>
        </div>
        <a href="/?view=printer-guide" class="btn">How to print</a>
    </header>

    {#if error}
        <div class="message error">{error}</div>
    {/if}
    {#if notice}
        <div class="message success">{notice}</div>
    {/if}

    {#if !loaded}
        <p class="muted">Loading printers…</p>
    {:else if printers.length === 0}
        <p class="muted">No printers are configured.</p>
    {:else}
        <div class="grid printers">
            {#each printers as printer (printer.id)}
                {@const status = chip(printer)}
                <div class="card">
                    <div class="head">
                        <div class="head-text">
                            <h2>{printer.name}</h2>
                            <p class="small muted">{availability(printer)}</p>
                        </div>
                        <span class="chip {status.cls}" style={status.hue ? `--hue: ${status.hue}` : undefined}>{stateLabel(printer)}</span>
                    </div>

                    {#if printer.access_code_problem}
                        <div class="code-warning">
                            <strong>Camera not connecting</strong>
                            <p>
                                {printer.name} accepted the connection and then hung up. Usually
                                the access code changed - toggling LAN mode on the printer
                                regenerates it. It also happens when the camera is already open
                                in Bambu Studio or Handy, since the P1S allows only one viewer.
                            </p>
                            {#if can(user, 'manage_printers')}
                                {#if editingCode === printer.id}
                                    <div class="code-form">
                                        <input
                                            type="text"
                                            bind:value={newCode}
                                            placeholder="New access code from the printer screen"
                                            autocomplete="off"
                                        />
                                        <button class="btn primary" on:click={() => saveAccessCode(printer)} disabled={savingCode}>
                                            {savingCode ? 'Saving…' : 'Save'}
                                        </button>
                                        <button class="btn" on:click={() => (editingCode = '')}>Cancel</button>
                                    </div>
                                    <p class="small muted">
                                        On the printer: Settings → Network → the code is shown with
                                        the IP address. No restart needed - it reconnects by itself.
                                    </p>
                                {:else}
                                    <button class="btn" on:click={() => startCodeEdit(printer)}>Update access code</button>
                                {/if}
                            {:else}
                                <p class="small muted">An admin can fix this from this page.</p>
                            {/if}
                        </div>
                    {/if}

                    <div class="camera">
                        {#if printer.camera_online && $camera[printer.id]}
                            <img src={$camera[printer.id]} alt="Camera view of {printer.name}" />
                        {:else}
                            <div class="camera-placeholder">
                                <img src="/P1S.png" alt="" class="placeholder-icon" />
                                <p>{printer.camera_online ? 'Loading camera…' : 'No camera image'}</p>
                            </div>
                        {/if}
                    </div>

                    {#if printer.online}
                        {#if isPrinting(printer)}
                            <div class="bar" style="--hue: var(--ctp-green)"><span style="width: {printer.progress}%"></span></div>
                            <div class="progress-meta mono">
                                <span>{printer.progress}%</span>
                                <span class="muted">{formatRemaining(printer.remaining_minutes)}</span>
                            </div>
                        {/if}

                        {#if printer.file_name}
                            <p class="file small muted" title={printer.file_name}>{tidyFileName(printer.file_name)}</p>
                        {/if}

                        {#if (printer.ams && printer.ams.length > 0) || printer.external_spool}
                            <div class="ams">
                                {#each printer.ams as unit}
                                    <div class="ams-head small">
                                        <span>AMS {unit.id + 1}</span>
                                        {#if unit.humidity}
                                            <span class="mono muted" title="The AMS reports humidity on its own 1-5 scale">Humidity {unit.humidity}/5</span>
                                        {/if}
                                        {#if unit.temp}
                                            <span class="mono muted">{unit.temp}°C</span>
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
                                                    <span class="swatch" style="background: {slot.color || 'transparent'}"></span>
                                                    <span class="mono faint">{slot.slot}</span>
                                                </span>
                                                <span class="ams-mat">{slot.empty ? 'Empty' : slot.material}</span>
                                                {#if slot.remain >= 0}
                                                    <span class="mono muted">{slot.remain}%</span>
                                                {:else if !slot.empty}
                                                    <span class="mono faint" title="No RFID tag, so the printer cannot tell">?</span>
                                                {/if}
                                            </div>
                                        {/each}
                                    </div>
                                {/each}

                                {#if printer.external_spool && !printer.external_spool.empty}
                                    <div class="ams-ext small">
                                        <span class="swatch" style="background: {printer.external_spool.color || 'transparent'}"></span>
                                        <span class="muted">External spool</span>
                                        <span class="ams-mat">{printer.external_spool.material}</span>
                                        {#if printer.external_spool.remain >= 0}
                                            <span class="mono muted">{printer.external_spool.remain}%</span>
                                        {/if}
                                    </div>
                                {/if}
                            </div>
                        {/if}

                        <div class="temps small mono muted">
                            <span><span class="dot" style="--hue: var(--ctp-peach)"></span>Nozzle {printer.nozzle_temp.toFixed(0)}°C</span>
                            <span><span class="dot" style="--hue: var(--ctp-maroon)"></span>Bed {printer.bed_temp.toFixed(0)}°C</span>
                            {#if printer.chamber_temp > 0}
                                <span><span class="dot" style="--hue: var(--ctp-sky)"></span>Chamber {printer.chamber_temp.toFixed(0)}°C</span>
                            {/if}
                        </div>
                        {#if printer.last_action_by}
                            <p class="small muted">Last stopped by {printer.last_action_by}</p>
                        {/if}

                        <button class="btn send-toggle" on:click={() => toggleSend(printer)}>
                            {sendFor === printer.id ? 'Close' : 'Send a file to print'}
                        </button>

                        {#if sendFor === printer.id}
                            <div class="send-panel small">
                                <p>
                                    <strong>Start the file name with your name</strong>, for example
                                    <code>srinath_bracket.3mf</code>, so everyone knows whose print it is.
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
                                        <span class="drop-title">Sending… <span class="mono">{uploadProgress}%</span></span>
                                        <div class="bar upload"><span style="width: {uploadProgress}%"></span></div>
                                    {:else}
                                        <span class="drop-title">Drop a sliced file here</span>
                                        <span class="muted">or click to choose · .3mf or .gcode</span>
                                    {/if}
                                </div>

                                <p class="muted">
                                    Slice in Bambu Studio and export the file. It lands on the
                                    printer; then go to the machine, check the plate is clear,
                                    and start it from the screen.
                                </p>

                                {#if printerFiles[printer.id]}
                                    {#if printerFiles[printer.id].length === 0}
                                        <p class="muted">No files on this printer yet.</p>
                                    {:else}
                                        <p class="label">On this printer</p>
                                        {#each printerFiles[printer.id].slice(0, 8) as file}
                                            <div class="row">
                                                <span class="send-file" title={file.name}>{file.name}</span>
                                                <span class="mono muted">{fileSize(file.size)}</span>
                                            </div>
                                        {/each}
                                    {/if}
                                {/if}
                            </div>
                        {/if}
                    {:else}
                        <p class="small muted">Not responding. It may be switched off or off the network.</p>
                    {/if}
                </div>
            {/each}
        </div>

        <p class="small muted footnote">
            Status refreshes automatically. The camera updates about once every two
            seconds, the fastest the P1S allows over the local network.
        </p>
    {/if}
</div>

<style>
    .printers {
        grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr));
    }

    .head {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        gap: 10px;
        margin-bottom: 14px;
    }

    .head-text {
        min-width: 0;
    }

    h2 {
        margin: 0;
        font-size: 1rem;
        font-weight: 600;
    }

    .card p {
        margin: 8px 0 0;
    }

    .head p {
        margin-top: 2px;
    }

    .small {
        font-size: 0.85rem;
    }

    .faint {
        color: var(--faint);
    }

    .chip {
        white-space: nowrap;
    }

    .chip.danger {
        color: var(--danger);
        border-color: color-mix(in srgb, var(--danger) 35%, transparent);
    }

    .title {
        display: flex;
        align-items: center;
        gap: 12px;
    }

    .code-warning {
        border: 1px solid var(--warn);
        border-radius: var(--radius);
        padding: 12px 14px;
        margin-bottom: 12px;
        font-size: 0.85rem;
        line-height: 1.4;
    }

    .code-warning p {
        color: var(--muted);
    }

    .code-warning > .btn {
        margin-top: 10px;
    }

    .code-form {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
        margin-top: 10px;
    }

    .code-form input {
        flex: 1;
        min-width: 150px;
        padding: 8px 10px;
    }

    .camera {
        aspect-ratio: 16 / 9;
        background: var(--card-raised);
        border: 1px solid var(--line);
        border-radius: var(--radius);
        overflow: hidden;
        margin-bottom: 14px;
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
        color: var(--faint);
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

    .progress-meta {
        display: flex;
        justify-content: space-between;
        margin-top: 6px;
        font-size: 0.85rem;
    }

    .file {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .temps {
        display: flex;
        flex-wrap: wrap;
        gap: 12px;
        margin-top: 12px;
    }

    .temps > span {
        display: inline-flex;
        align-items: center;
        gap: 6px;
    }

    .ams {
        margin-top: 14px;
        padding-top: 12px;
        border-top: 1px solid var(--line);
    }

    .ams-head {
        display: flex;
        gap: 12px;
        flex-wrap: wrap;
        margin-bottom: 8px;
        font-weight: 550;
    }

    .ams-slots {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(64px, 1fr));
        gap: 6px;
        margin-bottom: 8px;
    }

    .ams-slot {
        display: flex;
        flex-direction: column;
        gap: 3px;
        padding: 7px 8px;
        border: 1px solid var(--line);
        border-radius: var(--radius-sm);
        font-size: 0.78rem;
        min-width: 0;
    }

    .ams-slot.active {
        border-color: var(--accent);
    }

    .ams-slot.empty {
        opacity: 0.5;
    }

    .ams-top {
        display: flex;
        align-items: center;
        gap: 6px;
    }

    .swatch {
        width: 12px;
        height: 12px;
        border-radius: 50%;
        border: 1px solid var(--line-strong);
        flex-shrink: 0;
    }

    .ams-mat {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .ams-ext {
        display: flex;
        align-items: center;
        gap: 8px;
    }

    .ams-ext .ams-mat {
        flex: 1;
    }

    .send-toggle {
        width: 100%;
        margin-top: 14px;
    }

    .send-panel {
        margin-top: 12px;
        line-height: 1.45;
    }

    .send-panel p {
        margin: 0 0 10px;
    }

    code {
        font-family: var(--mono);
        background: var(--card-raised);
        border: 1px solid var(--line);
        border-radius: 4px;
        padding: 1px 5px;
    }

    .dropzone {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 4px;
        margin-bottom: 10px;
        padding: 22px 12px;
        border: 1px dashed var(--line-strong);
        border-radius: var(--radius);
        cursor: pointer;
        text-align: center;
        transition: border-color var(--fast) var(--ease), background-color var(--fast) var(--ease);
    }

    .dropzone:hover,
    .dropzone.over {
        border-color: var(--accent);
        background: var(--accent-soft);
    }

    .dropzone input {
        display: none;
    }

    .drop-title {
        font-weight: 600;
    }

    .upload {
        width: 100%;
        margin-top: 8px;
    }

    .send-panel .label {
        margin: 12px 0 0;
    }

    .row {
        padding: 6px 0;
    }

    .send-file {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .footnote {
        margin-top: 20px;
    }
</style>
