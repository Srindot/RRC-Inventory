<script>
    // The frame around every page: a sidebar on desktop, and on phones a slim
    // top bar plus a bottom tab bar with the five things people do most.
    import { onMount } from 'svelte';
    import { page } from '$app/stores';
    import { can, getUser, loginUrl, refreshUser } from '$lib/auth.js';
    import Icon from '$lib/Icon.svelte';
    import NotificationBell from '$lib/NotificationBell.svelte';
    import { AREA } from '$lib/palette.js';

    let user = null;
    let theme = 'dark';

    const nav = [
        { href: '/', label: 'Home', icon: 'home', hue: AREA.home },
        { href: '/dashboard', label: 'Dashboard', icon: 'grid', hue: AREA.dashboard, login: true },
        { href: '/mechlab', label: 'Mech Lab', icon: 'bot', hue: AREA.mech },
        { href: '/mocap', label: 'MoCap Lab', icon: 'video', hue: AREA.mocap },
        { href: '/items', label: 'Equipment', icon: 'box', hue: AREA.equipment },
        { href: '/threads', label: 'Research threads', icon: 'users', hue: AREA.threads, login: true },
        { href: '/printers', label: '3D printers', icon: 'printer', hue: AREA.printers }
    ];
    const tabs = [
        { href: '/', label: 'Home', icon: 'home', hue: AREA.home },
        { href: '/mechlab', label: 'Book', icon: 'calendar', hue: AREA.mech },
        { href: '/items', label: 'Equipment', icon: 'box', hue: AREA.equipment },
        { href: '/dashboard', label: 'Dashboard', icon: 'grid', hue: AREA.dashboard },
        { href: '/account', label: 'Me', icon: 'user', hue: AREA.account }
    ];

    $: path = $page.url.pathname;
    $: isAdmin = ['manage_users', 'manage_inventory', 'manage_printers', 'manage_bookings', 'approve_mech_bookings', 'approve_loans'].some((p) => can(user, p));
    $: initial = (user?.name || '?').trim().charAt(0).toUpperCase();
    $: bare = path === '/login' || path === '/register';

    const active = (href) => (href === '/' ? path === '/' : path === href || path.startsWith(href + '/'));
    // The phone "Book" tab covers both labs.
    const tabActive = (href) => active(href) || (href === '/mechlab' && active('/mocap'));

    onMount(async () => {
        theme = document.documentElement.dataset.theme || 'dark';
        // Follow the system until someone picks a theme themselves.
        const media = matchMedia('(prefers-color-scheme: light)');
        const follow = (e) => {
            try {
                if (localStorage.getItem('theme')) return;
            } catch {}
            setTheme(e.matches ? 'light' : 'dark', false);
        };
        media.addEventListener('change', follow);

        user = getUser();
        user = await refreshUser();
        return () => media.removeEventListener('change', follow);
    });

    function setTheme(next, remember = true) {
        theme = next;
        document.documentElement.dataset.theme = next;
        if (remember) {
            try {
                localStorage.setItem('theme', next);
            } catch {}
        }
    }

    // Read at click time: the shell outlives client-side navigation.
    const goLogin = () => (location.href = loginUrl());
</script>

{#if bare}
    <main class="bare"><slot /></main>
{:else}
    <aside class="sidebar">
        <a class="brand" href="/">
            <img src="/rrc_logo.png" alt="" width="26" height="26" />
            <span>RRC Lab</span>
        </a>

        <nav aria-label="Main">
            {#each nav.filter((n) => !n.login || user) as item (item.href)}
                <a class="nav-item" class:active={active(item.href)} style="--hue: {item.hue}" href={item.href} aria-current={active(item.href) ? 'page' : undefined}>
                    <Icon name={item.icon} />
                    <span>{item.label}</span>
                </a>
            {/each}
            {#if isAdmin}
                <a class="nav-item" class:active={active('/admin')} style="--hue: {AREA.admin}" href="/admin">
                    <Icon name="shield" />
                    <span>Admin</span>
                </a>
            {/if}
        </nav>

        <div class="sidebar-foot">
            {#if user}
                <a class="me" class:active={active('/account')} href="/account">
                    <span class="avatar">{initial}</span>
                    <span class="me-text">
                        <span class="me-name">{user.name}</span>
                        <span class="me-group">{user.group}</span>
                    </span>
                </a>
                <div class="foot-actions">
                    <NotificationBell up />
                    <button class="icon-btn" on:click={() => setTheme(theme === 'dark' ? 'light' : 'dark')} aria-label="Switch to {theme === 'dark' ? 'light' : 'dark'} mode">
                        <Icon name={theme === 'dark' ? 'sun' : 'moon'} />
                    </button>
                </div>
            {:else}
                <button class="nav-item" on:click={goLogin}>
                    <Icon name="login" />
                    <span>Log in</span>
                </button>
                <button class="icon-btn" on:click={() => setTheme(theme === 'dark' ? 'light' : 'dark')} aria-label="Switch to {theme === 'dark' ? 'light' : 'dark'} mode">
                    <Icon name={theme === 'dark' ? 'sun' : 'moon'} />
                </button>
            {/if}
        </div>
    </aside>

    <header class="topbar">
        <a class="brand" href="/">
            <img src="/rrc_logo.png" alt="" width="24" height="24" />
            <span>RRC Lab</span>
        </a>
        <div class="top-actions">
            <button class="icon-btn" on:click={() => setTheme(theme === 'dark' ? 'light' : 'dark')} aria-label="Switch to {theme === 'dark' ? 'light' : 'dark'} mode">
                <Icon name={theme === 'dark' ? 'sun' : 'moon'} />
            </button>
            {#if user}
                <NotificationBell />
                {#if isAdmin}
                    <a class="icon-btn" href="/admin" aria-label="Admin"><Icon name="shield" /></a>
                {/if}
            {:else}
                <button class="text-btn" on:click={goLogin}>Log in</button>
            {/if}
        </div>
    </header>

    <main class="content"><slot /></main>

    <nav class="tabbar" aria-label="Main">
        {#each tabs as tab (tab.href)}
            <a class="tab" class:active={tabActive(tab.href)} style="--hue: {tab.hue}" href={tab.href === '/account' && !user ? '/login' : tab.href}>
                <Icon name={tab.icon} size={20} />
                <span>{tab.label}</span>
            </a>
        {/each}
    </nav>
{/if}

<style>
    .sidebar {
        position: fixed;
        inset: 0 auto 0 0;
        width: var(--sidebar);
        display: flex;
        flex-direction: column;
        gap: 18px;
        padding: 18px 12px;
        box-sizing: border-box;
        background: var(--bg);
        border-right: 1px solid var(--line);
        z-index: 50;
    }

    .brand {
        display: flex;
        align-items: center;
        gap: 10px;
        padding: 4px 10px;
        color: var(--ctp-text);
        text-decoration: none;
        font-weight: 650;
        letter-spacing: -0.01em;
    }

    .brand img {
        border-radius: 6px;
    }

    nav {
        display: flex;
        flex-direction: column;
        gap: 2px;
    }

    .nav-item {
        position: relative;
        display: flex;
        align-items: center;
        gap: 12px;
        min-height: 40px;
        padding: 0 12px;
        border: none;
        border-radius: var(--radius-sm);
        background: transparent;
        color: var(--muted);
        font: inherit;
        font-size: 0.94rem;
        text-decoration: none;
        cursor: pointer;
        text-align: left;
    }

    .nav-item :global(svg) {
        color: var(--hue, var(--muted));
        opacity: 0.85;
    }

    .nav-item:hover {
        background: var(--line);
        color: var(--ctp-text);
    }

    .nav-item.active {
        background: color-mix(in srgb, var(--hue, var(--accent)) 14%, transparent);
        color: var(--ctp-text);
        font-weight: 550;
    }

    .nav-item.active :global(svg) {
        opacity: 1;
    }

    .nav-item.active::before {
        content: '';
        position: absolute;
        left: 0;
        top: 10px;
        bottom: 10px;
        width: 3px;
        border-radius: 3px;
        background: var(--hue, var(--accent));
    }

    .sidebar-foot {
        margin-top: auto;
        display: flex;
        flex-direction: column;
        gap: 8px;
        padding-top: 12px;
        border-top: 1px solid var(--line);
    }

    .me {
        display: flex;
        align-items: center;
        gap: 10px;
        padding: 8px 10px;
        border-radius: var(--radius-sm);
        color: var(--ctp-text);
        text-decoration: none;
    }

    .me:hover,
    .me.active {
        background: var(--line);
    }

    .avatar {
        display: grid;
        place-items: center;
        flex: none;
        width: 32px;
        height: 32px;
        border-radius: 50%;
        background: var(--accent);
        color: var(--accent-ink);
        font-weight: 650;
        font-size: 0.9rem;
    }

    .me-text {
        display: flex;
        flex-direction: column;
        min-width: 0;
    }

    .me-name {
        font-size: 0.9rem;
        font-weight: 550;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
    }

    .me-group {
        font-size: 0.78rem;
        color: var(--faint);
    }

    .foot-actions,
    .top-actions {
        display: flex;
        align-items: center;
        gap: 2px;
    }

    .icon-btn {
        display: grid;
        place-items: center;
        width: 40px;
        height: 40px;
        border: none;
        border-radius: var(--radius-sm);
        background: transparent;
        color: var(--muted);
        cursor: pointer;
    }

    .icon-btn:hover {
        background: var(--line);
        color: var(--ctp-text);
    }

    .text-btn {
        min-height: 36px;
        padding: 0 14px;
        border: 1px solid var(--line-strong);
        border-radius: var(--radius-sm);
        background: transparent;
        color: var(--ctp-text);
        font: inherit;
        font-weight: 550;
        cursor: pointer;
    }

    .content {
        margin-left: var(--sidebar);
        min-height: 100vh;
    }

    .bare {
        min-height: 100vh;
    }

    .topbar,
    .tabbar {
        display: none;
    }

    /* Phones and small tablets: top bar + bottom tabs instead of the sidebar */
    @media (max-width: 899px) {
        .sidebar {
            display: none;
        }

        .content {
            margin-left: 0;
        }

        .topbar {
            position: sticky;
            top: 0;
            z-index: 60;
            display: flex;
            align-items: center;
            justify-content: space-between;
            height: 56px;
            padding: 0 8px 0 12px;
            background: color-mix(in srgb, var(--bg) 88%, transparent);
            backdrop-filter: blur(12px);
            border-bottom: 1px solid var(--line);
        }

        .tabbar {
            position: fixed;
            left: 0;
            right: 0;
            bottom: 0;
            z-index: 60;
            display: grid;
            grid-template-columns: repeat(5, 1fr);
            padding: 6px 4px calc(6px + env(safe-area-inset-bottom));
            background: color-mix(in srgb, var(--bg) 92%, transparent);
            backdrop-filter: blur(12px);
            border-top: 1px solid var(--line);
        }

        .tab {
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 3px;
            min-height: 48px;
            justify-content: center;
            color: var(--faint);
            font-size: 0.7rem;
            text-decoration: none;
        }

        .tab.active {
            color: var(--hue, var(--accent));
        }
    }
</style>
