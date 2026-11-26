<script>
    // The frame around every page, in the lab's original style: the RRC logo
    // and name across the top, the page in a centred column, and a small
    // footer. The home page is the menu (big tiles); every other page gets a
    // "Back to Home" link instead of a sidebar, which reads the same on a
    // phone as on a desktop.
    import { onMount } from 'svelte';
    import { page } from '$app/stores';
    import { can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import DotBackground from '$lib/DotBackground.svelte';
    import Icon from '$lib/Icon.svelte';
    import NotificationBell from '$lib/NotificationBell.svelte';

    let user = null;
    let theme = 'dark';

    $: path = $page.url.pathname;
    // The home page also hosts the borrow/return/guide views. Tiles open them
    // with shallow routing, which changes page.state but not page.url, so
    // both are checked.
    $: view = $page.state?.view || $page.url.searchParams.get('view') || ($page.url.searchParams.has('borrow') ? 'borrow' : 'home');
    $: home = path === '/' && view === 'home';
    $: isAdmin = ['manage_users', 'manage_inventory', 'manage_printers', 'manage_bookings', 'approve_mech_bookings', 'approve_loans'].some((p) => can(user, p));
    $: initial = (user?.name || '?').trim().charAt(0).toUpperCase();
    $: firstName = (user?.name || '').trim().split(/\s+/)[0];

    onMount(async () => {
        theme = document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
        user = getUser();
        user = await refreshUser();
    });

    // The choice is remembered in this browser; app.html applies it before
    // the first paint on the next visit.
    function toggleTheme() {
        theme = theme === 'dark' ? 'light' : 'dark';
        document.documentElement.dataset.theme = theme;
        try {
            localStorage.setItem('theme', theme);
        } catch {}
    }

    // Read at click time: the shell outlives client-side navigation.
    const goLogin = () => (location.href = loginUrl());
</script>

<DotBackground />

<div class="frame">
    <header class="site-head" class:compact={!home}>
        <a class="brand" href="/" aria-label="Robotics Research Centre - home">
            <!-- One logo per theme: on light, the lettering is black instead of white -->
            <img class="logo logo-dark" src="/rrc_logo_header.png" alt="" width="384" height="168" />
            <img class="logo logo-light" src="/rrc_logo_header_light.png" alt="" width="384" height="168" />
            <span class="titles">
                <span class="site-title">Robotics Research Centre</span>
                <span class="site-sub">Lab Equipment Management System</span>
            </span>
        </a>

        <div class="actions">
            <button
                class="theme-toggle"
                on:click={toggleTheme}
                aria-label="Switch to {theme === 'dark' ? 'light' : 'dark'} mode"
                title="Switch to {theme === 'dark' ? 'light' : 'dark'} mode"
            >
                <Icon name={theme === 'dark' ? 'moon' : 'sun'} size={16} />
                <span>{theme === 'dark' ? 'Dark' : 'Light'}</span>
            </button>

            {#if user}
                <NotificationBell />
                {#if isAdmin}
                    <a class="head-btn" class:active={path.startsWith('/admin')} href="/admin" title="Admin">
                        <Icon name="shield" size={16} />
                        <span class="hide-sm">Admin</span>
                    </a>
                {/if}
                <a class="head-btn me" class:active={path.startsWith('/account')} href="/account" title="Your account">
                    <span class="avatar">{initial}</span>
                    <span class="hide-sm">{firstName}</span>
                </a>
            {:else if path !== '/login' && path !== '/register'}
                <button class="head-btn" on:click={goLogin}>
                    <Icon name="login" size={16} />
                    <span>Log in</span>
                </button>
            {/if}
        </div>
    </header>

    {#if !home}
        <nav class="crumbs" aria-label="Breadcrumb">
            <a href="/">← Back to Home</a>
        </nav>
    {/if}

    <main class="content" class:home><slot /></main>

    <footer class="site-foot">
        <span>Created by <a href="https://github.com/srindot" target="_blank" rel="noopener noreferrer">Srinath</a></span>
    </footer>
</div>

<style>
    .frame {
        position: relative;
        z-index: 1;
        display: flex;
        flex-direction: column;
        min-height: 100vh;
        min-height: 100dvh;
    }

    /* --- header ---------------------------------------------------------- */

    .site-head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        flex-wrap: wrap;
        gap: 14px 20px;
        width: 100%;
        max-width: var(--page-max);
        margin: 0 auto;
        padding: clamp(14px, 2.2vw, 36px) var(--gutter) clamp(6px, 1vw, 14px);
        box-sizing: border-box;
    }

    /* Logo and name share one frosted, semi-transparent box, so the name
       stays readable over the live background without hiding it */
    .brand {
        display: flex;
        align-items: center;
        gap: clamp(12px, 2.5vw, 18px);
        min-width: 0;
        padding: clamp(5px, 0.6vw, 8px) clamp(14px, 1.6vw, 22px) clamp(5px, 0.6vw, 8px) clamp(5px, 0.6vw, 8px);
        border: 1px solid var(--line);
        border-radius: clamp(14px, 1.4vw, 20px);
        background: color-mix(in srgb, var(--card) 55%, transparent);
        -webkit-backdrop-filter: blur(10px) saturate(1.15);
        backdrop-filter: blur(10px) saturate(1.15);
        box-shadow: var(--shadow-sm);
        color: inherit;
        text-decoration: none;
    }

    /* The dark-theme logo's lettering is white on transparent, so it sits on
       a dark Rosé Pine plate. The light theme swaps in a copy with black
       lettering (same blue badge), which needs no plate. */
    .logo {
        height: clamp(52px, min(4.6vw, 8vh), 88px);
        width: auto;
        padding: 3px 6px 3px 3px;
        border-radius: 12px;
        /* Semi-transparent dark plate: the white lettering still reads, and
           the frosted box shows through around it */
        background: color-mix(in srgb, #191724 55%, transparent);
        transition: transform var(--normal) var(--ease), box-shadow var(--normal) var(--ease);
    }

    .logo-light {
        display: none;
    }

    :global(:root[data-theme='light']) .logo-dark {
        display: none;
    }

    :global(:root[data-theme='light']) .logo-light {
        display: block;
        background: transparent;
        box-shadow: none;
    }

    .brand:hover .logo {
        transform: scale(1.04);
        box-shadow: 0 6px 18px color-mix(in srgb, var(--accent) 30%, transparent);
    }

    .titles {
        display: flex;
        flex-direction: column;
        gap: 2px;
        min-width: 0;
    }

    .site-title {
        font-size: clamp(1.35rem, 2.4vw, 2.6rem);
        font-weight: 600;
        letter-spacing: -0.025em;
        line-height: 1.15;
        color: var(--rp-text);
    }

    .site-sub {
        font-size: clamp(0.85rem, 1.1vw, 1.1rem);
        color: var(--muted);
        letter-spacing: 0.005em;
    }

    /* Inner pages: the same header, smaller, so the page itself leads */
    .site-head.compact .logo {
        height: clamp(40px, 3.6vw, 60px);
    }

    .site-head.compact .site-title {
        font-size: clamp(1.1rem, 1.6vw, 1.6rem);
    }

    .site-head.compact .site-sub {
        font-size: 0.85rem;
    }

    .actions {
        display: flex;
        align-items: center;
        gap: 8px;
        margin-left: auto;
    }

    .theme-toggle,
    .head-btn {
        display: inline-flex;
        align-items: center;
        gap: 8px;
        min-height: clamp(40px, 2.2vw, 48px);
        padding: 0 14px;
        border: 1px solid var(--line-strong);
        border-radius: var(--radius-pill);
        background: var(--glass);
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
        color: var(--rp-text);
        font: inherit;
        font-size: 0.9rem;
        font-weight: 550;
        text-decoration: none;
        cursor: pointer;
        transition:
            border-color var(--fast) var(--ease),
            background var(--fast) var(--ease),
            transform var(--fast) var(--ease);
    }

    .theme-toggle:active,
    .head-btn:active {
        transform: scale(0.97);
    }

    .theme-toggle :global(svg) {
        color: var(--rp-gold);
    }

    :global(:root[data-theme='dark']) .theme-toggle :global(svg) {
        color: var(--rp-iris);
    }

    .theme-toggle:hover,
    .head-btn:hover,
    .head-btn.active {
        border-color: color-mix(in srgb, var(--accent) 55%, var(--line-strong));
        background: var(--accent-soft);
    }

    .me {
        padding-left: 5px;
    }

    .avatar {
        display: grid;
        place-items: center;
        width: 30px;
        height: 30px;
        border-radius: 50%;
        background: var(--accent);
        color: var(--accent-ink);
        font-weight: 700;
        font-size: 0.85rem;
    }

    /* The bell is its own component; give it the same round outline */
    .actions :global(.bell-btn) {
        border: 1px solid var(--line-strong);
        border-radius: var(--radius-pill);
        background: var(--glass);
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    /* --- back link ------------------------------------------------------- */

    .crumbs {
        width: 100%;
        max-width: var(--page-max);
        margin: 0 auto;
        padding: 6px var(--gutter) 0;
        box-sizing: border-box;
    }

    .crumbs a {
        display: inline-flex;
        align-items: center;
        min-height: 36px;
        padding: 0 14px;
        margin-left: -2px;
        border: 1px solid var(--line);
        border-radius: var(--radius-pill);
        background: var(--glass);
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
        color: var(--link);
        text-decoration: none;
        font-size: 0.92rem;
        font-weight: 500;
        transition: border-color var(--fast) var(--ease), background var(--fast) var(--ease);
    }

    .crumbs a:hover {
        border-color: color-mix(in srgb, var(--link) 45%, var(--line-strong));
        background: color-mix(in srgb, var(--link) 10%, var(--glass));
    }

    /* --- page and footer ------------------------------------------------- */

    .content {
        flex: 1;
        width: 100%;
    }

    /* The home page stretches to fill the screen, so its tiles can sit
       balanced between the header and the footer */
    .content.home {
        display: flex;
        flex-direction: column;
    }

    .content.home > :global(.page) {
        flex: 1;
        display: flex;
        flex-direction: column;
        width: 100%;
    }

    .site-foot {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 2px;
        padding: 18px 16px calc(18px + env(safe-area-inset-bottom));
        border-top: 1px solid var(--line);
        background: var(--glass);
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
        color: var(--muted);
        font-size: 0.8rem;
        text-align: center;
    }

    .site-foot a {
        color: var(--rp-rose);
        text-decoration: none;
    }

    .site-foot a:hover {
        text-decoration: underline;
    }

    /* --- phones ---------------------------------------------------------- */

    @media (max-width: 640px) {
        /* Logo and name on one line, the buttons on the line below */
        .site-head {
            gap: 12px;
        }

        .actions {
            width: 100%;
            margin-left: 0;
            justify-content: flex-end;
        }

        .site-head:not(.compact) .brand {
            width: 100%;
        }

        .hide-sm {
            display: none;
        }

        .logo,
        .site-head.compact .logo {
            height: 44px;
        }

        .site-title,
        .site-head.compact .site-title {
            font-size: 1.15rem;
        }

        .site-sub,
        .site-head.compact .site-sub {
            font-size: 0.8rem;
        }

        .head-btn.me {
            padding: 0 5px;
        }
    }
</style>
