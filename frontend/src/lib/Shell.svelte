<script>
    // The frame around every page, in the lab's original style: the RRC logo
    // and name across the top, the page in a centred column, and a small
    // footer. The home page is the menu (big tiles); every other page gets a
    // "Back to Home" link instead of a sidebar, which reads the same on a
    // phone as on a desktop.
    import { onMount } from 'svelte';
    import { page } from '$app/stores';
    import { can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
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

    <main class="content"><slot /></main>

    <footer class="site-foot">
        <span>Created by <a href="https://github.com/srindot" target="_blank" rel="noopener noreferrer">Srinath</a></span>
    </footer>
</div>

<style>
    .frame {
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
        max-width: 1200px;
        margin: 0 auto;
        padding: clamp(14px, 3vw, 24px) clamp(16px, 3vw, 28px) 8px;
        box-sizing: border-box;
    }

    .brand {
        display: flex;
        align-items: center;
        gap: clamp(12px, 2.5vw, 18px);
        min-width: 0;
        color: inherit;
        text-decoration: none;
    }

    /* The dark-theme logo's lettering is white on transparent, so it sits on
       a dark Rosé Pine plate. The light theme swaps in a copy with black
       lettering (same blue badge), which needs no plate. */
    .logo {
        height: clamp(52px, 8vw, 78px);
        width: auto;
        padding: 3px 6px 3px 3px;
        border-radius: 12px;
        background: #191724;
        box-shadow: var(--shadow-sm);
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
        font-size: clamp(1.35rem, 3.6vw, 2.3rem);
        font-weight: 500;
        letter-spacing: -0.01em;
        line-height: 1.15;
        color: var(--rp-text);
    }

    .site-sub {
        font-size: clamp(0.85rem, 1.6vw, 1.05rem);
        color: var(--muted);
    }

    /* Inner pages: the same header, smaller, so the page itself leads */
    .site-head.compact .logo {
        height: clamp(40px, 5vw, 52px);
    }

    .site-head.compact .site-title {
        font-size: clamp(1.1rem, 2.4vw, 1.45rem);
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
        min-height: 40px;
        padding: 0 14px;
        border: 1px solid var(--line-strong);
        border-radius: var(--radius-pill);
        background: color-mix(in srgb, var(--card) 80%, transparent);
        color: var(--rp-text);
        font: inherit;
        font-size: 0.9rem;
        font-weight: 550;
        text-decoration: none;
        cursor: pointer;
        transition: border-color var(--fast) var(--ease), background var(--fast) var(--ease);
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
        background: color-mix(in srgb, var(--card) 80%, transparent);
    }

    /* --- back link ------------------------------------------------------- */

    .crumbs {
        width: 100%;
        max-width: 1200px;
        margin: 0 auto;
        padding: 6px clamp(16px, 3vw, 28px) 0;
        box-sizing: border-box;
    }

    .crumbs a {
        color: var(--link);
        text-decoration: none;
        font-size: 0.95rem;
    }

    .crumbs a:hover {
        text-decoration: underline;
    }

    /* --- page and footer ------------------------------------------------- */

    .content {
        flex: 1;
        width: 100%;
    }

    .site-foot {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 2px;
        padding: 18px 16px calc(18px + env(safe-area-inset-bottom));
        border-top: 1px solid var(--line);
        background: color-mix(in srgb, var(--rp-surface) 70%, transparent);
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
