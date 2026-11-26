<script>
    import { onMount } from 'svelte';

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

<main class="account-page auth">

    {#if done}
        <div class="account-card">
            <h1>Account created</h1>
            <p class="lead">A server admin approves new accounts, usually within a day. You can log in as soon as yours is approved. In a hurry? Ask a lab admin in the lab.</p>
            <a class="btn" href="/login">Go to log in</a>
        </div>
    {:else}
        <form class="account-card" on:submit|preventDefault={register}>
            <h1>Sign up</h1>
            <p class="lead">One account for borrowing equipment, booking labs and robots, and sending prints.</p>
            <label>
                Full name
                <input type="text" bind:value={form.name} required maxlength="100" autocomplete="name" />
            </label>
            <label>
                Email
                <input type="email" bind:value={form.email} required autocomplete="email" />
            </label>
            <label>
                Phone
                <input type="tel" inputmode="numeric" bind:value={form.phone} required autocomplete="tel" placeholder="10-digit mobile number" />
            </label>
            <label>
                I am a
                <select bind:value={form.group_id} required>
                    <option value="" disabled>Choose…</option>
                    {#each groups as g}
                        <option value={g.id}>{g.name}</option>
                    {/each}
                </select>
            </label>
            <label>
                Password <span class="hint">at least 8 characters</span>
                <span class="pw-row">
                    <input type={showPassword ? 'text' : 'password'} bind:value={form.password} required minlength="8" maxlength="72" autocomplete="new-password" />
                    <button type="button" class="btn" on:click={() => (showPassword = !showPassword)} aria-pressed={showPassword}>
                        {showPassword ? 'Hide' : 'Show'}
                    </button>
                </span>
            </label>
            {#if error}<p class="error" role="alert">{error}</p>{/if}
            <button type="submit" disabled={busy}>{busy ? 'Creating…' : 'Create account'}</button>
        </form>
        <p class="foot">Already have an account? <a href="/login">Log in</a></p>
    {/if}
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


    .hint {
        color: var(--faint);
        font-size: 0.85rem;
    }

    .pw-row {
        display: flex;
        gap: 8px;
        align-items: stretch;
        margin-top: 6px;
    }

    .pw-row input {
        flex: 1;
        min-width: 0;
        margin-top: 0;
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
