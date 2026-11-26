<script>
    import { onMount } from 'svelte';
    import { saveSession } from '$lib/auth.js';
    import IconEye from '@tabler/icons-svelte-runes/icons/eye';
    import IconEyeOff from '@tabler/icons-svelte-runes/icons/eye-off';
    import IconAlert from '@tabler/icons-svelte-runes/icons/alert-circle';
    import IconBorrow from '@tabler/icons-svelte-runes/icons/package-export';

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

<div class="container container-tight py-4">
    <div class="text-center mb-4">
        <a href="/" class="d-inline-block" aria-label="Robotics Research Centre - home">
            <img class="auth-logo hide-theme-light" src="/rrc_logo_header.png" alt="" width="384" height="168" />
            <img class="auth-logo hide-theme-dark" src="/rrc_logo_header_light.png" alt="" width="384" height="168" />
        </a>
    </div>

    <div class="card card-md">
        <div class="card-body">
            <h1 class="h2 text-center mb-1">Log in to your account</h1>
            <p class="text-center text-secondary mb-4">One account for borrowing, bookings and prints</p>

            <form on:submit|preventDefault={login} autocomplete="on">
                <div class="mb-3">
                    <label class="form-label" for="login-email">Email</label>
                    <input
                        id="login-email"
                        class="form-control"
                        type="text"
                        bind:value={email}
                        required
                        autocomplete="username"
                        placeholder="you@research.iiit.ac.in"
                    />
                </div>
                <div class="mb-3">
                    <label class="form-label" for="login-password">Password</label>
                    <div class="input-group input-group-flat">
                        <input
                            id="login-password"
                            class="form-control"
                            type={showPassword ? 'text' : 'password'}
                            bind:value={password}
                            required
                            autocomplete="current-password"
                        />
                        <span class="input-group-text">
                            <button
                                type="button"
                                class="btn btn-link link-secondary p-0"
                                on:click={() => (showPassword = !showPassword)}
                                aria-pressed={showPassword}
                                aria-label={showPassword ? 'Hide password' : 'Show password'}
                                title={showPassword ? 'Hide password' : 'Show password'}
                            >
                                {#if showPassword}<IconEyeOff size={18} class="icon" />{:else}<IconEye size={18} class="icon" />{/if}
                            </button>
                        </span>
                    </div>
                </div>

                {#if error}
                    <div class="alert alert-danger" role="alert">
                        <div class="d-flex gap-2">
                            <IconAlert size={20} class="icon alert-icon flex-shrink-0" />
                            <div>{error}</div>
                        </div>
                    </div>
                {/if}

                <div class="form-footer mt-3">
                    <button class="btn btn-primary w-100" type="submit" disabled={busy}>{busy ? 'Logging in…' : 'Log in'}</button>
                </div>
            </form>
        </div>
        <div class="hr-text my-0">or</div>
        <div class="card-body">
            <a class="btn btn-outline-secondary w-100" href="/?view=borrow">
                <IconBorrow size={18} class="icon" />
                Borrow without an account
            </a>
            <p class="form-hint text-center mt-2 mb-0">
                An account adds one-tap returns, lab and robot bookings, and notifications.
            </p>
        </div>
    </div>

    <div class="text-center text-secondary mt-3">
        No account yet? <a href="/register">Sign up</a>
    </div>
    <div class="text-center text-secondary small mt-1">Forgot your password? Ask a server admin to reset it.</div>
</div>

<style>
    .auth-logo {
        height: 72px;
        width: auto;
    }
</style>
