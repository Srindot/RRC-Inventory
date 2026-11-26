<script>
    import { onMount } from 'svelte';
    import { saveSession } from '$lib/auth.js';

    let email = '';
    let password = '';
    let error = '';
    let busy = false;
    let showPassword = false;
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

<div class="auth">
    <div class="auth-head">
        <h1>🔐 Log in</h1>
        <p class="subtitle">One account for borrowing, bookings and prints</p>
    </div>

    <form class="login-form" on:submit|preventDefault={login}>
        <div class="form-group">
            <label for="login-email">Email</label>
            <input id="login-email" type="text" bind:value={email} required autocomplete="username" placeholder="you@research.iiit.ac.in" />
        </div>
        <div class="form-group">
            <label for="login-password">Password</label>
            <div class="pw-row">
                <input id="login-password" type={showPassword ? 'text' : 'password'} bind:value={password} required autocomplete="current-password" />
                <button type="button" class="ghost-btn" on:click={() => (showPassword = !showPassword)} aria-pressed={showPassword}>
                    {showPassword ? 'Hide' : 'Show'}
                </button>
            </div>
        </div>
        {#if error}<p class="form-error" role="alert">{error}</p>{/if}
        <button class="submit-btn" type="submit" disabled={busy}>{busy ? 'Logging in…' : 'Log in'}</button>
    </form>

    <p class="foot">No account yet? <a href="/register">Sign up</a></p>
    <p class="foot">Forgot your password? Ask a server admin to reset it.</p>
    <p class="no-account">📦 Just borrowing something? <a href="/?view=borrow">Borrow without an account</a>. An account adds one-tap returns, lab and robot bookings, and notifications.</p>
</div>

<style>
    .auth {
        max-width: 450px;
        margin: clamp(10px, 5vh, 60px) auto 40px;
        padding: 0 16px;
        box-sizing: border-box;
    }

    .auth-head {
        text-align: center;
        margin-bottom: 20px;
    }

    h1 {
        margin: 0;
        font-size: clamp(1.6rem, 4vw, 2.1rem);
        font-weight: 600;
        color: var(--rp-iris);
    }

    .subtitle {
        margin: 4px 0 0;
        color: var(--ctp-subtext0);
        font-size: 0.95rem;
    }

    .login-form {
        background:
            linear-gradient(180deg, color-mix(in srgb, var(--rp-text) 4%, transparent), transparent 60%),
            color-mix(in srgb, var(--card) 60%, transparent);
        padding: clamp(22px, 5vw, 40px);
        border-radius: var(--radius-lg);
        border: 1px solid var(--ctp-surface0);
        box-shadow: var(--shadow-lg);
        margin-bottom: 18px;
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .form-group {
        margin-bottom: 20px;
    }

    .form-group label {
        display: block;
        margin-bottom: 8px;
        font-weight: 600;
        color: var(--ctp-text);
        font-size: 0.95rem;
    }

    .form-group input {
        width: 100%;
        min-height: 46px;
        padding: 10px 14px;
        border: 2px solid var(--ctp-surface0);
        border-radius: 8px;
        background: color-mix(in srgb, var(--bg) 60%, transparent);
        color: var(--ctp-text);
        font: inherit;
        box-sizing: border-box;
        transition: border-color var(--fast) var(--ease), box-shadow var(--fast) var(--ease);
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .form-group input:focus {
        border-color: var(--rp-rose);
        outline: none;
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--rp-rose) 25%, transparent);
    }

    .pw-row {
        display: flex;
        gap: 8px;
    }

    .pw-row input {
        flex: 1;
        min-width: 0;
    }

    .ghost-btn {
        min-height: 46px;
        padding: 0 14px;
        border: 1px solid var(--ctp-surface1);
        border-radius: 8px;
        background: color-mix(in srgb, var(--ctp-surface0) 60%, transparent);
        color: var(--ctp-text);
        font: inherit;
        cursor: pointer;
    }

    .ghost-btn:hover {
        background: color-mix(in srgb, var(--ctp-surface1) 60%, transparent);
    }

    .submit-btn {
        width: 100%;
        min-height: 48px;
        padding: 12px 20px;
        border: none;
        border-radius: 8px;
        background: linear-gradient(135deg, var(--rp-rose), var(--rp-love));
        color: var(--accent-ink);
        font: inherit;
        font-size: 1.05rem;
        font-weight: 600;
        cursor: pointer;
        transition: transform var(--fast) var(--ease), box-shadow var(--fast) var(--ease);
    }

    .submit-btn:hover {
        transform: translateY(-2px);
        box-shadow: 0 8px 25px color-mix(in srgb, var(--rp-rose) 30%, transparent);
    }

    .submit-btn:disabled {
        background: color-mix(in srgb, var(--ctp-surface0) 60%, transparent);
        color: var(--ctp-subtext0);
        cursor: wait;
        transform: none;
        box-shadow: none;
    }

    .form-error {
        margin: 0 0 14px;
        padding: 10px 14px;
        border-radius: 8px;
        border: 1px solid color-mix(in srgb, var(--danger) 40%, transparent);
        background: color-mix(in srgb, var(--danger) 12%, transparent);
        color: var(--danger);
        text-align: center;
        font-weight: 500;
    }

    .foot {
        margin: 6px 0 0;
        text-align: center;
        font-size: 0.9rem;
        color: var(--ctp-subtext0);
    }

    .foot a {
        color: var(--link);
        font-weight: 600;
    }

    .no-account {
        margin: 18px 0 0;
        padding: 12px 14px;
        border-radius: 8px;
        border: 1px dashed var(--ctp-surface1);
        font-size: 0.88rem;
        line-height: 1.45;
        color: var(--ctp-subtext0);
        text-align: center;
    }

    .no-account a {
        color: var(--link);
        font-weight: 600;
    }
</style>
