<script>
    // The frame around every page: Tabler's horizontal layout. The top bar
    // carries the RRC logo and name, the theme switch, notifications and the
    // account menu; the bar below it is the site menu, which folds behind the
    // menu button on phones.
    import { onMount } from 'svelte';
    import { page } from '$app/stores';
    import { api, can, clearSession, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import NotificationBell from '$lib/NotificationBell.svelte';
    import IconMenu from '@tabler/icons-svelte-runes/icons/menu-2';
    import IconX from '@tabler/icons-svelte-runes/icons/x';
    import IconSun from '@tabler/icons-svelte-runes/icons/sun';
    import IconMoon from '@tabler/icons-svelte-runes/icons/moon';
    import IconHome from '@tabler/icons-svelte-runes/icons/home';
    import IconBorrow from '@tabler/icons-svelte-runes/icons/package-export';
    import IconReturn from '@tabler/icons-svelte-runes/icons/package-import';
    import IconItems from '@tabler/icons-svelte-runes/icons/list-search';
    import IconPrinter from '@tabler/icons-svelte-runes/icons/printer';
    import IconVideo from '@tabler/icons-svelte-runes/icons/video';
    import IconRobot from '@tabler/icons-svelte-runes/icons/robot';
    import IconDashboard from '@tabler/icons-svelte-runes/icons/layout-dashboard';
    import IconThreads from '@tabler/icons-svelte-runes/icons/flask';
    import IconShield from '@tabler/icons-svelte-runes/icons/shield-lock';
    import IconUser from '@tabler/icons-svelte-runes/icons/user';
    import IconLogin from '@tabler/icons-svelte-runes/icons/login-2';
    import IconLogout from '@tabler/icons-svelte-runes/icons/logout';

    let user = null;
    let theme = 'dark';
    let menuOpen = false;
    let userMenu = false;

    $: path = $page.url.pathname;
    // The home page also hosts the borrow/return/guide views, opened either
    // with a ?view= link or with shallow routing (page.state), so both count.
    $: view = $page.state?.view || $page.url.searchParams.get('view') || ($page.url.searchParams.has('borrow') ? 'borrow' : 'home');
    $: isAdmin = ['manage_users', 'manage_inventory', 'manage_printers', 'manage_bookings', 'approve_mech_bookings', 'approve_loans'].some((p) => can(user, p));
    $: initial = (user?.name || '?').trim().charAt(0).toUpperCase();
    $: firstName = (user?.name || '').trim().split(/\s+/)[0];

    $: links = [
        { href: '/', label: 'Home', icon: IconHome, active: path === '/' && !['borrow', 'return'].includes(view) },
        { href: '/?view=borrow', label: 'Borrow', icon: IconBorrow, active: path === '/' && view === 'borrow' },
        { href: '/?view=return', label: 'Return', icon: IconReturn, active: path === '/' && view === 'return' },
        { href: '/items', label: 'Inventory', icon: IconItems, active: path.startsWith('/items') },
        { href: '/printers', label: '3D Printers', icon: IconPrinter, active: path.startsWith('/printers') },
        { href: '/mocap', label: 'MoCap Lab', icon: IconVideo, active: path.startsWith('/mocap') },
        { href: '/mechlab', label: 'Mech Lab', icon: IconRobot, active: path.startsWith('/mechlab') },
        ...(user
            ? [
                  { href: '/dashboard', label: 'Dashboard', icon: IconDashboard, active: path.startsWith('/dashboard') },
                  { href: '/threads', label: 'Threads', icon: IconThreads, active: path.startsWith('/threads') }
              ]
            : []),
        ...(isAdmin ? [{ href: '/admin', label: 'Admin', icon: IconShield, active: path.startsWith('/admin') }] : [])
    ];

    // Close the menus whenever the page changes
    $: $page.url, (menuOpen = false), (userMenu = false);

    onMount(async () => {
        theme = document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
        user = getUser();
        user = await refreshUser();
    });

    // Remembered in this browser; app.html applies it before the first paint
    // on the next visit.
    function toggleTheme() {
        theme = theme === 'dark' ? 'light' : 'dark';
        document.documentElement.dataset.theme = theme;
        document.documentElement.dataset.bsTheme = theme;
        try {
            localStorage.setItem('theme', theme);
        } catch {}
    }

    async function logout() {
        await api('/api/auth/logout', { method: 'POST' }).catch(() => {});
        clearSession();
        location.href = '/';
    }

    // Read at click time: the shell outlives client-side navigation.
    const goLogin = () => (location.href = loginUrl());

    function outside(e) {
        if (userMenu && !e.target.closest?.('.user-menu')) userMenu = false;
    }
</script>

<svelte:window on:click={outside} on:keydown={(e) => e.key === 'Escape' && ((userMenu = false), (menuOpen = false))} />

<div class="page">
    <header class="navbar navbar-expand-lg d-print-none top-bar">
        <div class="container-xl">
            <button
                class="navbar-toggler"
                type="button"
                aria-controls="site-menu"
                aria-expanded={menuOpen}
                aria-label="Toggle navigation"
                on:click={() => (menuOpen = !menuOpen)}
            >
                {#if menuOpen}<IconX size={22} />{:else}<IconMenu size={22} />{/if}
            </button>

            <a class="navbar-brand brand me-auto me-lg-0 pe-0 pe-lg-3" href="/" aria-label="Robotics Research Centre - home">
                <img class="logo hide-theme-light" src="/rrc_logo_header.png" alt="" width="384" height="168" />
                <img class="logo hide-theme-dark" src="/rrc_logo_header_light.png" alt="" width="384" height="168" />
                <span class="brand-text">
                    <span class="brand-title">Robotics Research Centre</span>
                    <span class="brand-sub">Lab Equipment Management</span>
                </span>
            </a>

            <div class="navbar-nav flex-row order-lg-last align-items-center gap-1 ms-auto">
                <button
                    class="btn btn-ghost-secondary btn-icon"
                    type="button"
                    on:click={toggleTheme}
                    aria-label="Switch to {theme === 'dark' ? 'light' : 'dark'} mode"
                    title="Switch to {theme === 'dark' ? 'light' : 'dark'} mode"
                >
                    {#if theme === 'dark'}<IconSun size={20} />{:else}<IconMoon size={20} />{/if}
                </button>

                {#if user}
                    <NotificationBell />
                    <div class="nav-item dropdown user-menu">
                        <button
                            class="nav-link d-flex lh-1 p-0 px-2 btn btn-link text-reset"
                            type="button"
                            aria-expanded={userMenu}
                            aria-label="Account menu"
                            on:click={() => (userMenu = !userMenu)}
                        >
                            <span class="avatar avatar-sm bg-primary-lt">{initial}</span>
                            <span class="d-none d-xl-block ps-2 text-start">
                                <span class="d-block fw-medium">{firstName}</span>
                                <span class="d-block small text-secondary mt-1">{user.group || 'Account'}</span>
                            </span>
                        </button>
                        <div class="dropdown-menu dropdown-menu-end dropdown-menu-arrow" class:show={userMenu}>
                            <span class="dropdown-header">{user.name}</span>
                            <a class="dropdown-item" href="/account"><IconUser size={18} class="icon dropdown-item-icon" />Your account</a>
                            {#if isAdmin}
                                <a class="dropdown-item" href="/admin"><IconShield size={18} class="icon dropdown-item-icon" />Admin</a>
                            {/if}
                            <div class="dropdown-divider"></div>
                            <button class="dropdown-item text-danger" type="button" on:click={logout}>
                                <IconLogout size={18} class="icon dropdown-item-icon" />Log out
                            </button>
                        </div>
                    </div>
                {:else if path !== '/login' && path !== '/register'}
                    <button class="btn btn-primary ms-1" type="button" on:click={goLogin}>
                        <IconLogin size={18} class="icon" />
                        <span class="d-none d-sm-inline">Log in</span>
                    </button>
                {/if}
            </div>
        </div>
    </header>

    <header class="navbar-expand-lg d-print-none">
        <div class="collapse navbar-collapse" class:show={menuOpen} id="site-menu">
            <div class="navbar menu-bar">
                <div class="container-xl">
                    <ul class="navbar-nav">
                        {#each links as link (link.href)}
                            <li class="nav-item" class:active={link.active}>
                                <a class="nav-link" href={link.href} aria-current={link.active ? 'page' : undefined}>
                                    <span class="nav-link-icon d-md-none d-lg-inline-block">
                                        <svelte:component this={link.icon} size={20} class="icon" />
                                    </span>
                                    <span class="nav-link-title">{link.label}</span>
                                </a>
                            </li>
                        {/each}
                    </ul>
                </div>
            </div>
        </div>
    </header>

    <div class="page-wrapper">
        <div class="page-body">
            <div class="container-xl">
                <slot />
            </div>
        </div>
    </div>
</div>

<style>
    .top-bar {
        border-bottom: 1px solid var(--tblr-border-color);
    }

    .menu-bar {
        border-bottom: 1px solid var(--tblr-border-color);
        background: var(--tblr-bg-surface);
        min-height: 3rem;
    }

    .brand {
        display: flex;
        align-items: center;
        gap: 0.75rem;
        min-width: 0;
        color: inherit;
        text-decoration: none;
    }

    .logo {
        height: 40px;
        width: auto;
    }

    /* The dark logo's lettering is white on transparent, which needs no
       plate on the dark theme; light swaps in the black-lettered copy. */
    .brand-text {
        display: flex;
        flex-direction: column;
        line-height: 1.2;
        min-width: 0;
    }

    .brand-title {
        font-size: 1rem;
        font-weight: 600;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .brand-sub {
        font-size: 0.75rem;
        font-weight: 400;
        color: var(--tblr-secondary-color);
    }

    .user-menu {
        position: relative;
    }

    .user-menu .dropdown-menu {
        position: absolute;
        right: 0;
        left: auto;
        top: calc(100% + 0.5rem);
    }

    @media (max-width: 575.98px) {
        .brand-sub {
            display: none;
        }

        .brand-title {
            font-size: 0.9rem;
        }

        .logo {
            height: 32px;
        }

        .brand {
            gap: 0.5rem;
        }
    }

    @media (max-width: 380px) {
        .brand-text {
            display: none;
        }
    }
</style>
