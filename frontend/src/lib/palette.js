// Colour with meaning: each area of the site and each research thread owns a
// Catppuccin hue, so people link things together without reading.

export const AREA = {
    home: 'var(--ctp-mauve)',
    dashboard: 'var(--ctp-blue)',
    mech: 'var(--ctp-peach)',
    mocap: 'var(--ctp-pink)',
    equipment: 'var(--ctp-teal)',
    threads: 'var(--ctp-green)',
    printers: 'var(--ctp-sapphire)',
    admin: 'var(--ctp-red)',
    account: 'var(--ctp-lavender)'
};

// The labs equipment lives in (loan and item records name them).
export const LAB = {
    'Main Lab': 'var(--ctp-blue)',
    'Mech Lab': AREA.mech,
    'Control Lab': 'var(--ctp-flamingo)'
};

const ROTATION = ['green', 'blue', 'peach', 'pink', 'teal', 'yellow', 'sapphire', 'mauve', 'flamingo', 'lavender', 'sky', 'maroon'];

// hueFor gives a stable colour for an id (a thread, a robot), so the same
// thread is the same colour on the calendar, its card and its hours bar.
export function hueFor(id) {
    const n = Math.abs(Number(id) || 0);
    return `var(--ctp-${ROTATION[n % ROTATION.length]})`;
}
