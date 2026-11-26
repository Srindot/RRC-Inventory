<script>
    import { onMount } from 'svelte';
    import { saveSession } from '$lib/auth.js';

    let email = '';
    let password = '';
    let error = '';
    let busy = false;
    let next = '/';

    onMount(() => {
        // Only same-site paths, so the link can't bounce someone elsewhere.
        // Resolving against our origin catches tricks like /\evil.com.
        const target = new URLSearchParams(location.search).get('next') || '/';
        try {
            const url = new URL(target, location.origin);
            // A path starting "//" (e.g. from "/.//evil.com") is read by the
            // browser as a link to another site, so it is refused as well.
            const local = url.origin === location.origin && !/^\/[\/\\]/.test(url.pathname);
            next = local ? url.pathname + url.search + url.hash : '/';
        } catch {
            next = '/';
        }
    });

    async function login() {
        busy = true;
        error = '';
        try {
            const response = await fetch('/api/auth/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ email, password })
            });
            const body = await response.json().catch(() => ({}));
            if (!response.ok) {
                error = body.error || `Login failed (HTTP ${response.status})`;
                return;
            }
            saveSession(body.token, body.user);
            location.href = next;
        } catch {
            error = 'Could not reach the server. Check your connection and try again.';
        } finally {
            busy = false;
        }
    }
</script>

<svelte:head><title>Log in · RRC</title></svelte:head>

<main class="account-page auth">
    <a class="brand" href="/">
        <img src="/rrc_logo.png" alt="" width="32" height="32" />
        <span>RRC Lab</span>
    </a>

    <form class="account-card" on:submit|preventDefault={login}>
        <h1>Log in</h1>
        <p class="lead">Robotics Research Centre</p>
        <label>
            Email
            <input type="text" bind:value={email} required autocomplete="username" />
        </label>
        <label>
            Password
            <input type="password" bind:value={password} required autocomplete="current-password" />
        </label>
        {#if error}<p class="error" role="alert">{error}</p>{/if}
        <button type="submit" disabled={busy}>{busy ? 'Logging in…' : 'Log in'}</button>
    </form>

    <p class="foot">No account yet? <a href="/register">Sign up</a>. Forgot your password? Ask a server admin to reset it.</p>
</main>

<style>
    /* Login and sign-up render without the shell: centred on desktop, top-aligned on phones */
    .auth {
        max-width: 400px;
        min-height: 100vh;
        box-sizing: border-box;
        display: flex;
        flex-direction: column;
        justify-content: center;
    }

    .auth h1 {
        font-size: 1.35rem;
    }

    .brand {
        display: flex;
        align-items: center;
        gap: 10px;
        margin: 0 0 20px;
        color: var(--ctp-text);
        text-decoration: none;
        font-weight: 650;
    }

    .brand img {
        border-radius: 6px;
    }

    .foot {
        margin: 0;
        font-size: 0.9rem;
        color: var(--muted);
    }

    .foot a {
        color: var(--accent);
    }

    @media (max-width: 700px) {
        .auth {
            justify-content: flex-start;
            padding-top: 32px;
        }
    }
</style>
