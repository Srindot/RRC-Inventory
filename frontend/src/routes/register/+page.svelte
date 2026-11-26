<script>
    import { onMount } from 'svelte';
    import IconEye from '@tabler/icons-svelte-runes/icons/eye';
    import IconEyeOff from '@tabler/icons-svelte-runes/icons/eye-off';
    import IconAlert from '@tabler/icons-svelte-runes/icons/alert-circle';
    import IconCheck from '@tabler/icons-svelte-runes/icons/circle-check';
    import IconBorrow from '@tabler/icons-svelte-runes/icons/package-export';

    let groups = [];
    let form = { name: '', email: '', phone: '', password: '', group_id: '' };
    let showPassword = false;
    let error = '';
    let done = false;
    let busy = false;

    onMount(async () => {
        try {
            const response = await fetch('/api/auth/groups');
            if (response.ok) groups = await response.json();
        } catch {
            error = 'Could not reach the server. Check your connection and try again.';
        }
    });

    async function register() {
        error = '';
        busy = true;
        try {
            const response = await fetch('/api/auth/register', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ...form, group_id: Number(form.group_id) })
            });
            const body = await response.json().catch(() => ({}));
            if (response.ok) {
                done = true;
            } else {
                error = body.error || `Sign-up failed (HTTP ${response.status})`;
            }
        } catch {
            error = 'Could not reach the server. Check your connection and try again.';
        } finally {
            busy = false;
        }
    }
</script>

<svelte:head><title>Sign up · RRC</title></svelte:head>

<div class="container container-tight py-4">
    <div class="text-center mb-4">
        <a href="/" class="d-inline-block" aria-label="Robotics Research Centre - home">
            <img class="auth-logo hide-theme-light" src="/rrc_logo_header.png" alt="" width="384" height="168" />
            <img class="auth-logo hide-theme-dark" src="/rrc_logo_header_light.png" alt="" width="384" height="168" />
        </a>
    </div>

    {#if done}
        <div class="card card-md" role="status">
            <div class="card-body text-center py-5">
                <IconCheck size={56} class="icon mb-3 text-success" />
                <h1 class="h2 mb-2">Account created</h1>
                <p class="text-secondary mb-4">
                    A server admin approves new accounts, usually within a day. You can log in as soon as yours is approved. In a hurry? Ask a lab admin in the lab.
                </p>
                <a class="btn btn-primary w-100" href="/login">Go to log in</a>
            </div>
        </div>
    {:else}
        <div class="card card-md">
            <div class="card-body">
                <h1 class="h2 text-center mb-1">Create an account</h1>
                <p class="text-center text-secondary mb-4">One account for borrowing equipment, booking labs and robots, and sending prints</p>

                <form on:submit|preventDefault={register}>
                    <div class="mb-3">
                        <label class="form-label" for="reg-name">Full name</label>
                        <input id="reg-name" class="form-control" type="text" bind:value={form.name} required maxlength="100" autocomplete="name" />
                    </div>
                    <div class="mb-3">
                        <label class="form-label" for="reg-email">Email</label>
                        <input id="reg-email" class="form-control" type="email" bind:value={form.email} required autocomplete="email" />
                    </div>
                    <div class="mb-3">
                        <label class="form-label" for="reg-phone">Phone</label>
                        <input
                            id="reg-phone"
                            class="form-control"
                            type="tel"
                            inputmode="numeric"
                            bind:value={form.phone}
                            required
                            autocomplete="tel"
                            placeholder="10-digit mobile number"
                        />
                    </div>
                    <div class="mb-3">
                        <label class="form-label" for="reg-group">I am a</label>
                        <select id="reg-group" class="form-select" bind:value={form.group_id} required>
                            <option value="" disabled>Choose…</option>
                            {#each groups as g}
                                <option value={g.id}>{g.name}</option>
                            {/each}
                        </select>
                    </div>
                    <div class="mb-3">
                        <label class="form-label" for="reg-password">
                            Password
                            <span class="form-label-description">at least 8 characters</span>
                        </label>
                        <div class="input-group input-group-flat">
                            <input
                                id="reg-password"
                                class="form-control"
                                class:is-invalid={form.password && form.password.length < 8}
                                class:is-valid={form.password.length >= 8}
                                type={showPassword ? 'text' : 'password'}
                                bind:value={form.password}
                                required
                                minlength="8"
                                maxlength="72"
                                autocomplete="new-password"
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
                        {#if form.password}
                            <div class="form-hint mt-1" class:text-danger={form.password.length < 8} class:text-success={form.password.length >= 8}>
                                {form.password.length < 8
                                    ? `${8 - form.password.length} more character${8 - form.password.length === 1 ? '' : 's'} needed`
                                    : 'Long enough'}
                            </div>
                        {/if}
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
                        <button class="btn btn-primary w-100" type="submit" disabled={busy}>{busy ? 'Creating…' : 'Create account'}</button>
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
            Already have an account? <a href="/login">Log in</a>
        </div>
    {/if}
</div>

<style>
    .auth-logo {
        height: 72px;
        width: auto;
    }
</style>
