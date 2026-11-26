<script>
    import { onMount, onDestroy } from 'svelte';
    import { cameraFeed } from '$lib/camera.js';
    import { api, can, getToken, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import IconBook from '@tabler/icons-svelte-runes/icons/book';
    import IconAlert from '@tabler/icons-svelte-runes/icons/alert-triangle';
    import IconLock from '@tabler/icons-svelte-runes/icons/lock';
    import IconExternal from '@tabler/icons-svelte-runes/icons/external-link';
    import IconBell from '@tabler/icons-svelte-runes/icons/bell';
    import IconBellOff from '@tabler/icons-svelte-runes/icons/bell-off';
    import IconBulb from '@tabler/icons-svelte-runes/icons/bulb';
    import IconBulbOff from '@tabler/icons-svelte-runes/icons/bulb-off';
    import IconPlay from '@tabler/icons-svelte-runes/icons/player-play';
    import IconPause from '@tabler/icons-svelte-runes/icons/player-pause';
    import IconStop from '@tabler/icons-svelte-runes/icons/player-stop';
    import IconUpload from '@tabler/icons-svelte-runes/icons/cloud-upload';
    import IconFolder from '@tabler/icons-svelte-runes/icons/folder';
    import IconFile from '@tabler/icons-svelte-runes/icons/file-3d';
    import IconTrash from '@tabler/icons-svelte-runes/icons/trash';
    import IconRefresh from '@tabler/icons-svelte-runes/icons/refresh';
    import IconLogin from '@tabler/icons-svelte-runes/icons/login-2';
    import IconX from '@tabler/icons-svelte-runes/icons/x';
    import IconDroplet from '@tabler/icons-svelte-runes/icons/droplet';
    import IconTemp from '@tabler/icons-svelte-runes/icons/temperature';
    import IconPrinter from '@tabler/icons-svelte-runes/icons/printer';

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

    // Remote control (light, pause/resume/stop, start a print). Keyed by
    // printer id, holding the command in flight, so every control on that
    // card is disabled until the printer answers.
    let busy = {};

    // Starting a print is the only thing here that makes a machine move on
    // its own, so the form asks for the plate explicitly rather than guessing,
    // and the confirm step spells out what is about to happen. Same options
    // and request body as the admin panel.
    let startFor = '';
    let startFile = '';
    let startBed = 'textured_plate';
    let startPlate = 1;
    // 'external', or the flat AMS tray number as a string
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

    // Firmware 01.10 with Authorization Control drops unsigned commands and
    // raises this fault ("MQTT command verification failed") instead.
    const COMMAND_REJECTED = '0500_0500_0001_0007';

    // "Notify me when done": printer ids this browser is watching, and the
    // state each printer was in at the previous refresh
    const NOTIFY_KEY = 'printerNotify';
    const ACTIVE_STATES = ['RUNNING', 'PREPARE', 'PAUSE'];
    let notifyOn = {};
    let lastSeen = {};
    // Browsers only allow desktop notifications on HTTPS (or localhost); on
    // plain http the in-page alert is all there is
    let secure = true;

    // The confirmation dialog: ask() opens it and resolves with the answer.
    // { title, lines, ok, tone, resolve }
    let confirmBox = null;

    function ask(box) {
        return new Promise((resolve) => {
            confirmBox = { tone: 'danger', ...box, resolve };
        });
    }

    function answer(value) {
        const box = confirmBox;
        confirmBox = null;
        box?.resolve(value);
    }

    function onKey(e) {
        if (e.key !== 'Escape') return;
        if (confirmBox) answer(false);
        else if (startFor) startFor = '';
    }

    // The printer whose start-print dialog is open
    $: startPrinter = printers.find((p) => p.id === startFor);

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
        notifyOn = loadNotify();
        secure = window.isSecureContext !== false;

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
                const next = await response.json();
                watchForFinish(next);
                printers = next;
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
            const sent = (result.body.message || 'File sent to the printer').replace(/\.?\s*$/, '.');
            showNotice(`${sent} You'll get a notification when it finishes.`);
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
        const ok = await ask({
            title: `Delete ${file.name}?`,
            lines: [`It will be removed from ${printer.name}. This cannot be undone.`],
            ok: 'Delete'
        });
        if (!ok) return;
        await adminDelete(
            printer,
            `${printer.id}/${file.name}`,
            `/api/admin/printers/${printer.id}/files/${encodeURIComponent(file.name)}`,
            `${file.name} deleted from ${printer.name}`
        );
    }

    async function clearAllFiles(printer) {
        const count = printerFiles[printer.id]?.length || 0;
        const ok = await ask({
            title: `Delete all ${count} file(s)?`,
            lines: [`Every file on ${printer.name} will be removed. This cannot be undone.`],
            ok: 'Delete all'
        });
        if (!ok) return;
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

    // --- remote control --------------------------------------------------

    // Every command goes through here so the messages stay consistent
    async function control(printer, path, options, okMessage) {
        if (busy[printer.id]) return false;
        busy = { ...busy, [printer.id]: path };
        try {
            const response = await api(`/api/admin/printers/${printer.id}/${path}`, options);
            if (response.status === 401) {
                user = null;
                showError('Your session expired. Log in again.');
                return false;
            }
            const result = await response.json().catch(() => ({}));
            if (response.ok) {
                showNotice(okMessage || result.message || 'Command sent', 6000);
                loadPrinters();
                later(loadPrinters, 2500);
                return true;
            }
            if (response.status === 403) {
                showError("Your account can't control printers. Ask a server admin.");
            } else {
                showError(result.error || `The printer refused that command (HTTP ${response.status})`);
            }
            return false;
        } catch (e) {
            showError('Could not reach the server');
            return false;
        } finally {
            busy = { ...busy, [printer.id]: '' };
        }
    }

    function toggleLight(printer) {
        const on = !printer.light_on;
        control(
            printer,
            'light',
            {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ on })
            },
            `${printer.name}: light ${on ? 'on' : 'off'}`
        );
    }

    function pausePrint(printer) {
        control(printer, 'pause', { method: 'POST' }, `Paused ${printer.name}`);
    }

    function resumePrint(printer) {
        control(printer, 'resume', { method: 'POST' }, `Resumed ${printer.name}`);
    }

    async function stopPrint(printer) {
        const job = tidyFileName(printer.file_name) || 'the current job';
        const ok = await ask({
            title: `Stop ${job} on ${printer.name}?`,
            lines: ['This cannot be undone - the print will be cancelled.'],
            ok: 'Stop print'
        });
        if (!ok) return;
        control(printer, 'stop', { method: 'POST' }, `Stop command sent to ${printer.name}`);
    }

    // Running, or about to: pause/stop make sense
    function isActive(printer) {
        return printer.online && ACTIVE_STATES.includes(printer.state);
    }

    function canStart(printer) {
        return printer.online && ['IDLE', 'FINISH', 'FAILED'].includes(printer.state);
    }

    // The firmware wants one flat, zero based tray number across every unit:
    // the first AMS holds 0-3, the second 4-7. The UI counts slots from 1
    // because that is what is printed on the machine.
    function amsChoices(printer) {
        const choices = [];
        for (const unit of printer.ams || []) {
            for (const slot of unit.slots || []) {
                if (slot.empty) continue;
                choices.push({
                    index: unit.id * 4 + (slot.slot - 1),
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
        // The first loaded AMS slot, or the external spool with no AMS fitted
        const slots = amsChoices(printer);
        startSpool = slots.length > 0 ? String(slots[0].index) : 'external';
        loadPrinterFiles(printer);
    }

    function printThisFile(printer, name) {
        if (startFor !== printer.id) toggleStart(printer);
        startFile = name;
    }

    async function startPrint(printer) {
        if (!startFile) {
            showError('Choose a file to print', 6000);
            return;
        }

        const bed = BED_TYPES.find((b) => b.value === startBed)?.label || startBed;
        const useAMS = startSpool !== 'external';
        const slot = useAMS ? Number(startSpool) : 0;
        const chosen = amsChoices(printer).find((c) => c.index === slot);
        let spool = 'the external spool';
        if (useAMS) {
            spool = chosen ? `AMS ${chosen.unit} slot ${chosen.slot} — ${chosen.label}` : `AMS tray ${slot}`;
        }

        const ok = await ask({
            title: `Start ${startFile} on ${printer.name}?`,
            lines: [
                `Plate: ${bed}`,
                `Filament: ${spool}`,
                'The printer will start moving. Check the plate is clear and the right one is fitted before continuing.'
            ],
            ok: 'Start print',
            tone: 'primary'
        });
        if (!ok) return;

        const file = startFile;
        const started = await control(
            printer,
            'print',
            {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    file_name: file,
                    plate: Number(startPlate) || 1,
                    bed_type: startBed,
                    use_ams: useAMS,
                    ams_slot: slot,
                    bed_levelling: startLevel,
                    flow_cali: startFlow,
                    timelapse: startTimelapse
                })
            },
            `Started ${file} on ${printer.name}`
        );
        if (started) startFor = '';
    }

    // Bambu writes codes with or without the HMS_ prefix
    function isRejected(printer) {
        return (printer.faults || []).some(
            (f) => (f.code || '').toUpperCase().replace(/^HMS_/, '') === COMMAND_REJECTED
        );
    }

    // --- "notify me when done" ------------------------------------------

    function loadNotify() {
        try {
            const saved = JSON.parse(localStorage.getItem(NOTIFY_KEY) || '{}');
            return saved && typeof saved === 'object' ? saved : {};
        } catch (e) {
            return {};
        }
    }

    function saveNotify() {
        try {
            localStorage.setItem(NOTIFY_KEY, JSON.stringify(notifyOn));
        } catch (e) {
            // Private window or blocked storage: the toggle lasts this visit
        }
    }

    async function toggleNotify(printer) {
        if (notifyOn[printer.id]) {
            const rest = { ...notifyOn };
            delete rest[printer.id];
            notifyOn = rest;
            saveNotify();
            return;
        }

        notifyOn = { ...notifyOn, [printer.id]: true };
        saveNotify();

        let permission = typeof Notification === 'undefined' ? 'unsupported' : Notification.permission;
        if (permission === 'default') {
            try {
                permission = await Notification.requestPermission();
            } catch (e) {
                permission = 'denied';
            }
        }

        if (permission === 'granted') {
            showNotice(`You'll get a notification when ${printer.name} is done - keep this page open.`, 6000);
        } else if (permission === 'unsupported') {
            showNotice(`This browser can't show notifications - you'll see an alert on this page when ${printer.name} is done.`);
        } else {
            showNotice(`Notifications are blocked for this site - you'll see an alert on this page when ${printer.name} is done.`);
        }
    }

    // Compare each printer with the previous refresh, and announce the end
    // of a job on the printers this browser is watching
    function watchForFinish(next) {
        for (const p of next) {
            const before = lastSeen[p.id];
            if (
                notifyOn[p.id] &&
                before &&
                ACTIVE_STATES.includes(before.state) &&
                ['FINISH', 'FAILED'].includes(p.state)
            ) {
                announce(p, before.file);
            }
        }
        lastSeen = Object.fromEntries(next.map((p) => [p.id, { state: p.state, file: p.file_name }]));
    }

    function announce(printer, previousFile) {
        const job = tidyFileName(printer.file_name || previousFile);
        const failed = printer.state === 'FAILED';
        const title = failed
            ? `${printer.name} failed - check it`
            : `${printer.name} finished${job ? ' ' + job : ''}`;

        if (failed) showError(title, 15000);
        else showNotice(title, 15000);

        if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
            try {
                new Notification(title, {
                    body: failed
                        ? `${job || 'The job'} stopped before it was done.`
                        : 'Collect it from the lab and clear the plate.',
                    icon: '/P1S.png',
                    tag: `printer-${printer.id}`,
                    silent: true
                });
            } catch (e) {
                // Some mobile browsers only allow notifications from a
                // service worker; the in-page alert above still shows
            }
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

    const BADGE = {
        running: 'bg-blue-lt',
        failed: 'bg-red-lt',
        paused: 'bg-yellow-lt',
        finished: 'bg-green-lt',
        idle: 'bg-teal-lt',
        offline: 'bg-secondary-lt'
    };

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

<svelte:window on:keydown={onKey} />

<div class="page-header d-print-none mt-0 mb-3">
    <div class="row g-2 align-items-center">
        <div class="col">
            <div class="page-pretitle">Lab equipment</div>
            <h2 class="page-title">3D Printers</h2>
            <div class="text-secondary mt-1">Live status of the lab's Bambu Lab P1S printers</div>
        </div>
        <div class="col-12 col-sm-auto">
            <a href="/?view=printer-guide" class="btn btn-outline-primary w-100">
                <IconBook size={18} class="icon" />
                How to print
            </a>
        </div>
    </div>
</div>

{#if error}
    <div class="toast-msg error" role="status">{error}</div>
{/if}
{#if notice}
    <div class="toast-msg success" class:stacked={!!error} role="status">{notice}</div>
{/if}

{#if !loaded}
    <div class="card">
        <div class="card-body text-secondary">Loading printers...</div>
    </div>
{:else if printers.length === 0}
    <div class="card">
        <div class="empty">
            <div class="empty-icon"><IconPrinter size={48} class="icon" /></div>
            <p class="empty-title">No printers are configured</p>
        </div>
    </div>
{:else}
    <div class="d-flex flex-wrap gap-3 mb-3" aria-label="Printer summary">
        <span class="status status-green"><span class="status-dot"></span>{summary.free} free</span>
        <span class="status status-blue"><span class="status-dot status-dot-animated"></span>{summary.printing} printing</span>
        {#if summary.attention}
            <span class="status status-yellow"><span class="status-dot"></span>{summary.attention} paused or failed</span>
        {/if}
        {#if summary.offline}
            <span class="status status-secondary"><span class="status-dot"></span>{summary.offline} offline</span>
        {/if}
    </div>

    <div class="row row-cards g-3">
        {#each printers as printer (printer.id)}
            <div class="col-12 col-lg-6">
                <div class="card h-100">
                    <div class="card-header">
                        <img src="/P1S.png" alt="" class="printer-icon me-2" width="32" height="32" />
                        <div class="min-w-0">
                            <h3 class="card-title text-truncate">{printer.name}</h3>
                            <div class="card-subtitle mt-1">
                                <span class="status {isPrinting(printer) ? 'status-blue' : printer.online ? 'status-green' : 'status-secondary'}">
                                    <span class="status-dot"></span>{availability(printer)}
                                </span>
                            </div>
                        </div>
                        <div class="card-actions">
                            <span class="badge {BADGE[stateClass(printer)]}">{stateLabel(printer)}</span>
                        </div>
                    </div>

                    <div class="ratio ratio-16x9 camera">
                        {#if printer.camera_online && $camera[printer.id]}
                            <img src={$camera[printer.id]} alt="Camera view of {printer.name}" />
                        {:else}
                            <div class="d-flex flex-column align-items-center justify-content-center text-secondary">
                                <img src="/P1S.png" alt="" class="camera-placeholder mb-2" width="64" height="64" />
                                <span class="small">{printer.camera_online ? 'Loading camera...' : 'No camera image'}</span>
                            </div>
                        {/if}
                    </div>

                    <div class="card-body">
                        {#if printer.access_code_problem}
                            <div class="alert alert-warning" role="alert">
                                <div class="d-flex">
                                    <div><IconAlert size={20} class="icon alert-icon" /></div>
                                    <div class="min-w-0 flex-fill">
                                        <h4 class="alert-title">Camera not connecting</h4>
                                        <div class="text-secondary">
                                            {printer.name} accepted the connection and then hung up. Usually
                                            the access code changed - toggling LAN mode on the printer
                                            regenerates it. It also happens when the camera is already open
                                            in Bambu Studio or Handy, since the P1S allows only one viewer.
                                        </div>
                                        {#if canManage}
                                            {#if editingCode === printer.id}
                                                <div class="input-group mt-2">
                                                    <input
                                                        type="text"
                                                        class="form-control"
                                                        bind:value={newCode}
                                                        placeholder="New access code"
                                                        aria-label="New access code from the printer screen"
                                                        autocomplete="off"
                                                        on:keydown={(e) => e.key === 'Enter' && saveAccessCode(printer)}
                                                    />
                                                    <button class="btn btn-primary" on:click={() => saveAccessCode(printer)} disabled={savingCode}>
                                                        {savingCode ? 'Saving...' : 'Save'}
                                                    </button>
                                                    <button class="btn" on:click={() => (editingCode = '')}>Cancel</button>
                                                </div>
                                                <div class="form-hint mt-1">
                                                    On the printer: Settings › Network - the code is shown with
                                                    the IP address. No restart needed - it reconnects by itself.
                                                </div>
                                            {:else}
                                                <button class="btn btn-warning btn-sm mt-2" on:click={() => startCodeEdit(printer)}>
                                                    Update access code
                                                </button>
                                            {/if}
                                        {:else}
                                            <div class="form-hint mt-1">A printer admin can fix this from this page.</div>
                                        {/if}
                                    </div>
                                </div>
                            </div>
                        {/if}

                        {#if isRejected(printer)}
                            <div class="alert alert-warning" role="alert">
                                <div class="d-flex">
                                    <div><IconLock size={20} class="icon alert-icon" /></div>
                                    <div>
                                        {#if canManage}
                                            <h4 class="alert-title">Remote control blocked by the printer</h4>
                                            <div class="text-secondary">
                                                This printer rejected a command from the website: its firmware
                                                only accepts signed commands. Turn on
                                                <strong>LAN Only Mode › Developer Mode</strong> on the printer's
                                                screen (Settings › Network) to allow remote control.
                                            </div>
                                        {:else}
                                            <h4 class="alert-title">Remote control is off</h4>
                                            <div class="text-secondary">
                                                {printer.name} isn't accepting commands from the website yet. Use
                                                the printer's screen - a printer admin is sorting it out.
                                            </div>
                                        {/if}
                                    </div>
                                </div>
                            </div>
                        {/if}

                        {#if printer.faults && printer.faults.length > 0}
                            <div class="alert alert-danger" role="alert">
                                <div class="d-flex">
                                    <div><IconAlert size={20} class="icon alert-icon" /></div>
                                    <div class="min-w-0 flex-fill">
                                        <h4 class="alert-title">
                                            {printer.faults.length} fault{printer.faults.length > 1 ? 's' : ''} reported
                                        </h4>
                                        <ul class="list-unstyled mb-0">
                                            {#each printer.faults as fault (fault.code)}
                                                <li class="d-flex align-items-center gap-2 flex-wrap mt-1">
                                                    <span class="badge {fault.severity === 'fatal' || fault.severity === 'serious' ? 'bg-red-lt' : 'bg-yellow-lt'} text-uppercase">{fault.severity}</span>
                                                    <code class="text-break">{fault.code}</code>
                                                    <a href={fault.url} target="_blank" rel="noopener" class="ms-auto">
                                                        Look up <IconExternal size={14} class="icon" />
                                                    </a>
                                                </li>
                                            {/each}
                                        </ul>
                                    </div>
                                </div>
                            </div>
                        {/if}

                        {#if printer.online}
                            {#if isPrinting(printer)}
                                <div class="d-flex align-items-center gap-2 mb-1">
                                    <div
                                        class="progress progress-sm flex-fill"
                                        role="progressbar"
                                        aria-label="Print progress"
                                        aria-valuenow={printer.progress}
                                        aria-valuemin="0"
                                        aria-valuemax="100"
                                    >
                                        <div class="progress-bar" style="width: {printer.progress}%"></div>
                                    </div>
                                    <span class="fw-medium small">{printer.progress}%</span>
                                </div>
                            {/if}

                            {#if printer.file_name}
                                <div class="d-flex align-items-center gap-1 text-secondary small mb-3 min-w-0" title={printer.file_name}>
                                    <IconFile size={16} class="icon flex-shrink-0" />
                                    <span class="text-truncate">{tidyFileName(printer.file_name)}</span>
                                </div>
                            {/if}

                            <div class="datagrid mb-3">
                                <div class="datagrid-item">
                                    <div class="datagrid-title">Nozzle</div>
                                    <div class="datagrid-content">{printer.nozzle_temp.toFixed(0)}°C</div>
                                </div>
                                <div class="datagrid-item">
                                    <div class="datagrid-title">Bed</div>
                                    <div class="datagrid-content">{printer.bed_temp.toFixed(0)}°C</div>
                                </div>
                                {#if printer.chamber_temp > 0}
                                    <div class="datagrid-item">
                                        <div class="datagrid-title">Chamber</div>
                                        <div class="datagrid-content">{printer.chamber_temp.toFixed(0)}°C</div>
                                    </div>
                                {/if}
                                {#if isPrinting(printer) && printer.remaining_minutes > 0}
                                    <div class="datagrid-item">
                                        <div class="datagrid-title">Time left</div>
                                        <div class="datagrid-content">{formatRemaining(printer.remaining_minutes)}</div>
                                    </div>
                                    <div class="datagrid-item">
                                        <div class="datagrid-title">Finishes</div>
                                        <div class="datagrid-content">{finishTime(printer.remaining_minutes)}</div>
                                    </div>
                                {/if}
                            </div>

                            {#if (printer.ams && printer.ams.length > 0) || printer.external_spool}
                                {#each printer.ams || [] as unit}
                                    <div class="d-flex align-items-center gap-3 mb-2 small">
                                        <span class="subheader mb-0">AMS {unit.id + 1}</span>
                                        {#if unit.humidity}
                                            <span class="text-secondary d-inline-flex align-items-center gap-1" title="The AMS reports humidity on its own 1-5 scale">
                                                <IconDroplet size={14} class="icon" />{unit.humidity}/5
                                            </span>
                                        {/if}
                                        {#if unit.temp}
                                            <span class="text-secondary d-inline-flex align-items-center gap-1">
                                                <IconTemp size={14} class="icon" />{unit.temp}°C
                                            </span>
                                        {/if}
                                    </div>

                                    <div class="row g-2 mb-3">
                                        {#each unit.slots as slot}
                                            <div class="col-6 col-sm-3">
                                                <div
                                                    class="slot"
                                                    class:active={slot.active}
                                                    title={slot.empty
                                                        ? `Slot ${slot.slot}: empty`
                                                        : `Slot ${slot.slot}: ${slot.material}${slot.remain >= 0 ? ', ' + slot.remain + '% left' : ', amount unknown'}${slot.active ? ' (in use)' : ''}`}
                                                >
                                                    <div class="d-flex align-items-center gap-2">
                                                        <span class="swatch" class:empty={slot.empty} style="background: {slot.empty ? 'transparent' : slot.color || 'transparent'}"></span>
                                                        <span class="fw-medium">{slot.slot}</span>
                                                        {#if slot.active}<span class="status-dot status-dot-animated status-green ms-auto" title="Currently feeding"></span>{/if}
                                                    </div>
                                                    <div class="small mt-1 text-truncate" class:text-secondary={slot.empty}>
                                                        {slot.empty ? 'Empty' : slot.material}
                                                    </div>
                                                    {#if slot.remain >= 0}
                                                        <div class="d-flex align-items-center gap-1 mt-1">
                                                            <div class="progress progress-sm flex-fill">
                                                                <div class="progress-bar bg-secondary" style="width: {slot.remain}%"></div>
                                                            </div>
                                                            <span class="small text-secondary">{slot.remain}%</span>
                                                        </div>
                                                    {:else if !slot.empty}
                                                        <div class="small text-secondary mt-1" title="No RFID tag, so the printer cannot tell">? left</div>
                                                    {/if}
                                                </div>
                                            </div>
                                        {/each}
                                    </div>
                                {/each}

                                {#if printer.external_spool && !printer.external_spool.empty}
                                    <div class="d-flex align-items-center gap-2 mb-3 small">
                                        <span class="swatch" style="background: {printer.external_spool.color || 'transparent'}"></span>
                                        <span class="fw-medium">External spool</span>
                                        <span class="text-secondary">{printer.external_spool.material}</span>
                                        {#if printer.external_spool.remain >= 0}
                                            <span class="text-secondary ms-auto">{printer.external_spool.remain}%</span>
                                        {/if}
                                    </div>
                                {/if}
                            {/if}

                            {#if printer.last_action_by}
                                <div class="small text-secondary">Last stopped by {printer.last_action_by}</div>
                            {/if}

                            {#if notifyOn[printer.id] && !secure}
                                <div class="form-hint mt-2">
                                    Desktop notifications need HTTPS, so on this address you'll see an
                                    alert on this page instead. Keep the page open.
                                </div>
                            {/if}

                            {#if sendFor === printer.id}
                                <div class="files mt-3 pt-3">
                                    {#if canUpload}
                                        <p class="small mb-2">
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
                                                class="d-none"
                                                type="file"
                                                accept=".3mf,.gcode"
                                                on:change={(e) => handlePick(printer, e)}
                                            />
                                            {#if uploading === printer.id}
                                                <span class="fw-medium">Sending... {uploadProgress}%</span>
                                                <div
                                                    class="progress progress-sm w-100 mt-2"
                                                    role="progressbar"
                                                    aria-label="Upload progress"
                                                    aria-valuenow={uploadProgress}
                                                    aria-valuemin="0"
                                                    aria-valuemax="100"
                                                >
                                                    <div class="progress-bar" style="width: {uploadProgress}%"></div>
                                                </div>
                                            {:else}
                                                <IconUpload size={28} class="icon text-primary mb-1" />
                                                <span class="fw-medium">Drop a sliced file here</span>
                                                <span class="small text-secondary">or tap to choose - .3mf or .gcode</span>
                                            {/if}
                                        </div>

                                        <div class="form-hint mt-2">
                                            Slice in Bambu Studio and export the file. It lands on the
                                            printer - then go to the machine, check the plate is clear,
                                            and start it from the screen.
                                        </div>
                                    {:else if !user}
                                        <div class="alert alert-info mb-0">
                                            <div>Log in to send a sliced file to {printer.name} from here.</div>
                                            <a class="btn btn-primary btn-sm mt-2" href={loginHref}>
                                                <IconLogin size={16} class="icon" /> Log in to send files
                                            </a>
                                        </div>
                                    {:else}
                                        <div class="alert alert-info mb-0">
                                            Your account can't send files to printers yet. Ask a
                                            server admin for the upload permission.
                                        </div>
                                    {/if}

                                    <div class="d-flex align-items-center gap-2 mt-3 mb-2">
                                        <span class="subheader mb-0 me-auto">
                                            On this printer{printerFiles[printer.id] ? ` (${printerFiles[printer.id].length})` : ''}
                                        </span>
                                        <button
                                            class="btn btn-ghost-secondary btn-sm"
                                            on:click={() => loadPrinterFiles(printer)}
                                            disabled={filesLoading[printer.id]}
                                            title="Read the file list again"
                                        >
                                            <IconRefresh size={16} class="icon" />
                                            {filesLoading[printer.id] ? '...' : 'Refresh'}
                                        </button>
                                        {#if canManage && printerFiles[printer.id]?.length > 0}
                                            <button
                                                class="btn btn-ghost-danger btn-sm"
                                                on:click={() => clearAllFiles(printer)}
                                                disabled={!!deleting || isPrinting(printer)}
                                                title={isPrinting(printer) ? 'Wait for the job to finish' : 'Delete every file on this printer'}
                                            >
                                                <IconTrash size={16} class="icon" />
                                                {deleting === `${printer.id}/*` ? 'Clearing...' : 'Clear all'}
                                            </button>
                                        {/if}
                                    </div>

                                    {#if filesError[printer.id]}
                                        <div class="text-danger small mb-2">{filesError[printer.id]}</div>
                                    {/if}

                                    {#if printerFiles[printer.id]}
                                        {#if printerFiles[printer.id].length === 0}
                                            <div class="text-secondary small">No files on this printer yet.</div>
                                        {:else}
                                            <div class="list-group list-group-flush file-list">
                                                {#each showAllFiles[printer.id] ? printerFiles[printer.id] : printerFiles[printer.id].slice(0, FILES_SHOWN) as file (file.name)}
                                                    <div class="list-group-item px-0 d-flex align-items-center gap-2">
                                                        <div class="min-w-0 flex-fill">
                                                            <div class="text-truncate" title={file.name}>{file.name}</div>
                                                            <div class="small text-secondary">
                                                                {fileSize(file.size)}{file.time ? ` · ${file.time}` : ''}
                                                                {#if isCurrentJob(printer, file)}<span class="text-primary"> · printing</span>{/if}
                                                            </div>
                                                        </div>
                                                        {#if canManage && canStart(printer)}
                                                            <button
                                                                class="btn btn-ghost-primary btn-icon"
                                                                on:click={() => printThisFile(printer, file.name)}
                                                                disabled={!!busy[printer.id]}
                                                                title="Print {file.name} on {printer.name}"
                                                                aria-label="Print {file.name}"
                                                            >
                                                                <IconPlay size={18} class="icon" />
                                                            </button>
                                                        {/if}
                                                        {#if canManage}
                                                            <button
                                                                class="btn btn-ghost-danger btn-icon"
                                                                on:click={() => deleteFile(printer, file)}
                                                                disabled={!!deleting || isCurrentJob(printer, file)}
                                                                title={isCurrentJob(printer, file) ? 'This file is printing right now' : `Delete ${file.name}`}
                                                                aria-label="Delete {file.name}"
                                                            >
                                                                {#if deleting === `${printer.id}/${file.name}`}
                                                                    <span class="spinner-border spinner-border-sm" aria-hidden="true"></span>
                                                                {:else}
                                                                    <IconTrash size={18} class="icon" />
                                                                {/if}
                                                            </button>
                                                        {/if}
                                                    </div>
                                                {/each}
                                            </div>
                                            {#if printerFiles[printer.id].length > FILES_SHOWN}
                                                <button
                                                    class="btn btn-link btn-sm px-0 mt-1"
                                                    on:click={() => (showAllFiles = { ...showAllFiles, [printer.id]: !showAllFiles[printer.id] })}
                                                >
                                                    {showAllFiles[printer.id]
                                                        ? 'Show fewer'
                                                        : `Show all ${printerFiles[printer.id].length} files`}
                                                </button>
                                            {/if}
                                        {/if}
                                    {:else if filesLoading[printer.id]}
                                        <div class="text-secondary small">Reading the printer's files...</div>
                                    {/if}
                                </div>
                            {/if}
                        {:else}
                            <div class="text-secondary">
                                Not responding - it may be switched off or off the network.
                                This page keeps checking and updates by itself when it is back.
                            </div>
                        {/if}
                    </div>

                    {#if printer.online}
                        <div class="card-footer">
                            <div class="btn-list" aria-label="{printer.name} controls">
                                <button
                                    class="btn {notifyOn[printer.id] ? 'btn-primary' : 'btn-outline-secondary'}"
                                    aria-pressed={!!notifyOn[printer.id]}
                                    on:click={() => toggleNotify(printer)}
                                    title="Get a notification in this browser when the current job finishes or fails"
                                >
                                    {#if notifyOn[printer.id]}
                                        <IconBell size={18} class="icon" /> Notifying you
                                    {:else}
                                        <IconBellOff size={18} class="icon" /> Notify me when done
                                    {/if}
                                </button>

                                {#if canManage}
                                    <button
                                        class="btn {printer.light_on ? 'btn-warning' : 'btn-outline-secondary'}"
                                        aria-pressed={!!printer.light_on}
                                        on:click={() => toggleLight(printer)}
                                        disabled={!!busy[printer.id]}
                                        title="Chamber light - the camera is useless in the dark"
                                    >
                                        {#if printer.light_on}<IconBulb size={18} class="icon" />{:else}<IconBulbOff size={18} class="icon" />{/if}
                                        {busy[printer.id] === 'light' ? '...' : printer.light_on ? 'Light on' : 'Light off'}
                                    </button>

                                    {#if printer.state === 'PAUSE'}
                                        <button class="btn btn-outline-success" on:click={() => resumePrint(printer)} disabled={!!busy[printer.id]}>
                                            <IconPlay size={18} class="icon" />
                                            {busy[printer.id] === 'resume' ? 'Resuming...' : 'Resume'}
                                        </button>
                                    {:else if isActive(printer)}
                                        <button class="btn btn-outline-warning" on:click={() => pausePrint(printer)} disabled={!!busy[printer.id]}>
                                            <IconPause size={18} class="icon" />
                                            {busy[printer.id] === 'pause' ? 'Pausing...' : 'Pause'}
                                        </button>
                                    {/if}

                                    {#if isActive(printer)}
                                        <button class="btn btn-outline-danger" on:click={() => stopPrint(printer)} disabled={!!busy[printer.id]}>
                                            <IconStop size={18} class="icon" />
                                            {busy[printer.id] === 'stop' ? 'Stopping...' : 'Stop'}
                                        </button>
                                    {/if}

                                    {#if canStart(printer)}
                                        <button
                                            class="btn btn-primary"
                                            aria-haspopup="dialog"
                                            on:click={() => toggleStart(printer)}
                                            disabled={!!busy[printer.id]}
                                        >
                                            <IconPlay size={18} class="icon" /> Start print
                                        </button>
                                    {/if}
                                {/if}

                                <button
                                    class="btn {sendFor === printer.id ? 'btn-secondary' : 'btn-outline-secondary'}"
                                    aria-expanded={sendFor === printer.id}
                                    on:click={() => toggleSend(printer)}
                                >
                                    {#if sendFor === printer.id}
                                        <IconX size={18} class="icon" /> Close files
                                    {:else if canUpload}
                                        <IconUpload size={18} class="icon" /> Send a file to print
                                    {:else}
                                        <IconFolder size={18} class="icon" /> Files on this printer
                                    {/if}
                                </button>
                            </div>
                        </div>
                    {/if}
                </div>
            </div>
        {/each}
    </div>

    <p class="text-secondary small mt-3 mb-0">
        Status refreshes automatically. The camera updates about once every two
        seconds - that is the fastest the P1S allows over the local network.
        {#if !secure}
            Desktop notifications need HTTPS; on this address "Notify me when done"
            shows an alert on this page instead.
        {/if}
    </p>
{/if}

{#if canManage && startPrinter && canStart(startPrinter)}
    {@const printer = startPrinter}
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions -->
    <div
        class="modal d-block"
        tabindex="-1"
        role="dialog"
        aria-modal="true"
        aria-labelledby="start-title"
        on:click|self={() => (startFor = '')}
    >
        <div class="modal-dialog modal-dialog-centered modal-dialog-scrollable" role="document">
            <div class="modal-content" id="start-{printer.id}">
                <div class="modal-header">
                    <h5 class="modal-title" id="start-title">Start a print on {printer.name}</h5>
                    <button type="button" class="btn-close" aria-label="Close" on:click={() => (startFor = '')}></button>
                </div>
                <div class="modal-body">
                    <div class="mb-3">
                        <label class="form-label" for="start-file">File</label>
                        <select id="start-file" class="form-select" bind:value={startFile}>
                            <option value="">
                                {printerFiles[printer.id]
                                    ? printerFiles[printer.id].length
                                        ? 'Choose a file...'
                                        : 'No files on this printer'
                                    : filesError[printer.id]
                                        ? 'Could not read the files'
                                        : 'Loading files...'}
                            </option>
                            {#each printerFiles[printer.id] || [] as file (file.name)}
                                <option value={file.name}>{file.name}</option>
                            {/each}
                        </select>
                    </div>

                    <div class="row g-3 mb-3">
                        <div class="col-sm-8">
                            <label class="form-label" for="start-bed">Plate fitted</label>
                            <select id="start-bed" class="form-select" bind:value={startBed}>
                                {#each BED_TYPES as bed}<option value={bed.value}>{bed.label}</option>{/each}
                            </select>
                        </div>
                        <div class="col-sm-4">
                            <label class="form-label" for="start-plate">Plate number</label>
                            <input id="start-plate" class="form-control" type="number" min="1" max="64" bind:value={startPlate} />
                        </div>
                    </div>

                    <div class="mb-3">
                        <label class="form-label" for="start-spool">Filament</label>
                        <select id="start-spool" class="form-select" bind:value={startSpool}>
                            {#each amsChoices(printer) as choice}
                                <option value={String(choice.index)}>
                                    AMS {choice.unit} slot {choice.slot} — {choice.label}{choice.remain >= 0 ? ` (${choice.remain}%)` : ''}
                                </option>
                            {/each}
                            <option value="external">External spool</option>
                        </select>
                        {#if startSpool !== 'external'}
                            <div class="d-flex flex-wrap gap-2 mt-2">
                                {#each amsChoices(printer) as choice}
                                    <button
                                        type="button"
                                        class="btn btn-sm {startSpool === String(choice.index) ? 'btn-primary' : 'btn-outline-secondary'}"
                                        title="AMS {choice.unit} slot {choice.slot} — {choice.label}"
                                        aria-label="AMS {choice.unit} slot {choice.slot}: {choice.label}"
                                        aria-pressed={startSpool === String(choice.index)}
                                        on:click={() => (startSpool = String(choice.index))}
                                    >
                                        <span class="swatch me-1" style="background: {choice.color || 'var(--ctp-surface1)'}"></span>
                                        {choice.unit}.{choice.slot}
                                    </button>
                                {/each}
                            </div>
                        {/if}
                    </div>

                    <div class="mb-3">
                        <label class="form-check form-switch">
                            <input class="form-check-input" type="checkbox" bind:checked={startLevel} />
                            <span class="form-check-label">Bed levelling</span>
                        </label>
                        <label class="form-check form-switch">
                            <input class="form-check-input" type="checkbox" bind:checked={startFlow} />
                            <span class="form-check-label">Flow calibration</span>
                        </label>
                        <label class="form-check form-switch mb-0">
                            <input class="form-check-input" type="checkbox" bind:checked={startTimelapse} />
                            <span class="form-check-label">Timelapse</span>
                        </label>
                    </div>

                    <div class="alert alert-warning mb-0">
                        <div class="d-flex">
                            <div><IconAlert size={20} class="icon alert-icon" /></div>
                            <div>
                                Check the camera: the plate must be clear and the plate type must
                                match what you chose.
                            </div>
                        </div>
                    </div>
                </div>
                <div class="modal-footer">
                    <button type="button" class="btn me-auto" on:click={() => (startFor = '')}>Cancel</button>
                    <button
                        type="button"
                        class="btn btn-primary"
                        on:click={() => startPrint(printer)}
                        disabled={!!busy[printer.id] || !startFile}
                    >
                        <IconPlay size={18} class="icon" />
                        {busy[printer.id] === 'print' ? 'Starting...' : 'Start print'}
                    </button>
                </div>
            </div>
        </div>
    </div>
    <div class="modal-backdrop show"></div>
{/if}

{#if confirmBox}
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions -->
    <div
        class="modal d-block"
        tabindex="-1"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        on:click|self={() => answer(false)}
    >
        <div class="modal-dialog modal-sm modal-dialog-centered" role="document">
            <div class="modal-content">
                <div class="modal-status bg-{confirmBox.tone}"></div>
                <div class="modal-body text-center py-4">
                    <IconAlert size={40} class="icon mb-2 text-{confirmBox.tone}" />
                    <h3 id="confirm-title" class="text-break">{confirmBox.title}</h3>
                    {#each confirmBox.lines || [] as line}
                        <div class="text-secondary mt-1">{line}</div>
                    {/each}
                </div>
                <div class="modal-footer">
                    <div class="w-100">
                        <div class="row g-2">
                            <div class="col">
                                <button type="button" class="btn w-100" on:click={() => answer(false)}>Cancel</button>
                            </div>
                            <div class="col">
                                <button type="button" class="btn btn-{confirmBox.tone} w-100" on:click={() => answer(true)}>
                                    {confirmBox.ok}
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>
    <div class="modal-backdrop show"></div>
{/if}

<style>
    .min-w-0 {
        min-width: 0;
    }

    .printer-icon {
        width: 32px;
        height: 32px;
        object-fit: contain;
        flex-shrink: 0;
    }

    .camera {
        background: var(--tblr-bg-surface-secondary);
        border-bottom: 1px solid var(--tblr-border-color);
    }

    .camera img:not(.camera-placeholder) {
        object-fit: cover;
    }

    .camera-placeholder {
        position: static;
        width: 64px;
        height: 64px;
        object-fit: contain;
        opacity: 0.5;
    }

    .slot {
        height: 100%;
        padding: 0.5rem;
        border: 1px solid var(--tblr-border-color);
        border-radius: var(--tblr-border-radius);
    }

    .slot.active {
        border-color: var(--tblr-primary);
        box-shadow: 0 0 0 1px var(--tblr-primary);
    }

    .swatch {
        display: inline-block;
        flex-shrink: 0;
        width: 1rem;
        height: 1rem;
        border-radius: 50%;
        border: 1px solid var(--tblr-border-color-translucent, var(--tblr-border-color));
        vertical-align: -0.15em;
    }

    .swatch.empty {
        border-style: dashed;
    }

    .files {
        border-top: 1px solid var(--tblr-border-color);
    }

    .file-list {
        max-height: 26rem;
        overflow-y: auto;
    }

    .dropzone {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 0.15rem;
        padding: 1.25rem 1rem;
        border: 2px dashed var(--tblr-border-color);
        border-radius: var(--tblr-border-radius-lg, 8px);
        text-align: center;
        cursor: pointer;
        transition: border-color 0.15s, background-color 0.15s;
    }

    .dropzone:hover,
    .dropzone:focus-visible,
    .dropzone.over {
        border-color: var(--tblr-primary);
        background: color-mix(in srgb, var(--tblr-primary) 8%, transparent);
        outline: none;
    }

    .dropzone.busy {
        cursor: progress;
    }

    .toast-msg.stacked {
        bottom: 80px;
    }

    .modal {
        overflow-y: auto;
    }
</style>
