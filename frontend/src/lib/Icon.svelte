<script>
    // Line icons (24px grid, 1.75 stroke), drawn in the style of Lucide (ISC).
    // Kept inline so there is no icon dependency to maintain.
    export let name;
    export let size = 18;

    const paths = {
        home: 'M3 10.5 12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z',
        grid: 'M3 3h7v7H3zM14 3h7v7h-7zM14 14h7v7h-7zM3 14h7v7H3z',
        calendar: 'M4 5h16v16H4zM16 3v4M8 3v4M4 10h16',
        bot: 'M12 8V4H9M5 8h14v11a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2zM9 13v2M15 13v2',
        video: 'M15 10l6-3.5v11L15 14M3 6h12v12H3z',
        box: 'M21 8 12 3 3 8v8l9 5 9-5zM3 8l9 5 9-5M12 13v8',
        users: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM22 21v-2a4 4 0 0 0-3-3.9M16 3.1a4 4 0 0 1 0 7.8',
        printer: 'M6 9V3h12v6M6 18H4a1 1 0 0 1-1-1v-6a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v6a1 1 0 0 1-1 1h-2M6 14h12v7H6z',
        shield: 'M12 21s8-3.5 8-10V5l-8-3-8 3v6c0 6.5 8 10 8 10z',
        user: 'M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2M12 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8z',
        bell: 'M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.9 1.9 0 0 0 3.4 0',
        sun: 'M12 16a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M6.3 17.7l-1.4 1.4M19.1 4.9l-1.4 1.4',
        moon: 'M12 3a6.5 6.5 0 0 0 9 9 9 9 0 1 1-9-9z',
        login: 'M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4M10 17l5-5-5-5M15 12H3',
        arrow: 'M5 12h14M13 6l6 6-6 6'
    };
</script>

<svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.75"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
>
    <path d={paths[name]} />
</svg>
