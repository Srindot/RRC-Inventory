import { writable } from 'svelte/store';

// Camera frames for the printer cards, one object URL per printer id.
//
// Pointing an <img> straight at /snapshot and bumping a ?t= every two seconds
// shows a broken image whenever one fetch misses - and the printers drop their
// camera connection every few minutes, so misses are routine. Here each frame
// is fetched first and only swapped in once it has arrived, so a miss just
// leaves the last good picture up.
export function cameraFeed() {
    let current = {};
    let destroyed = false;
    const frames = writable(current);
    const inFlight = new Set();

    async function load(id) {
        if (inFlight.has(id)) return;
        inFlight.add(id);
        try {
            const response = await fetch(`/api/printers/${id}/snapshot`, { cache: 'no-store' });
            if (!response.ok) return;
            const blob = await response.blob();
            if (destroyed || blob.size === 0) return;

            const old = current[id];
            current = { ...current, [id]: URL.createObjectURL(blob) };
            frames.set(current);
            if (old) URL.revokeObjectURL(old);
        } catch (e) {
            // Keep showing the last frame; the next refresh tries again
        } finally {
            inFlight.delete(id);
        }
    }

    // Fetch a fresh frame for every printer whose camera is up
    function refresh(printers) {
        if (destroyed) return;
        for (const printer of printers) {
            if (printer.camera_online) load(printer.id);
        }
    }

    function destroy() {
        destroyed = true;
        for (const url of Object.values(current)) URL.revokeObjectURL(url);
        current = {};
        frames.set(current);
    }

    return { subscribe: frames.subscribe, refresh, destroy };
}
