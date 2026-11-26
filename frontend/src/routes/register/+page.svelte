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

<div class="auth">
    <div class="auth-head">
        <h1>✍️ Sign up</h1>
        <p class="subtitle">One account for borrowing equipment, booking labs and robots, and sending prints</p>
    </div>

    {#if done}
        <div class="login-form done" role="status">
            <p class="done-icon" aria-hidden="true">✅</p>
            <h2>Account created</h2>
            <p>A server admin approves new accounts, usually within a day. You can log in as soon as yours is approved. In a hurry? Ask a lab admin in the lab.</p>
            <a class="submit-btn" href="/login">Go to log in</a>
        </div>
    {:else}
        <form class="login-form" on:submit|preventDefault={register}>
            <div class="form-group">
                <label for="reg-name">Full name</label>
                <input id="reg-name" type="text" bind:value={form.name} required maxlength="100" autocomplete="name" />
            </div>
            <div class="form-group">
                <label for="reg-email">Email</label>
                <input id="reg-email" type="email" bind:value={form.email} required autocomplete="email" />
            </div>
            <div class="form-group">
                <label for="reg-phone">Phone</label>
                <input id="reg-phone" type="tel" inputmode="numeric" bind:value={form.phone} required autocomplete="tel" placeholder="10-digit mobile number" />
            </div>
            <div class="form-group">
                <label for="reg-group">I am a</label>
                <select id="reg-group" bind:value={form.group_id} required>
                    <option value="" disabled>Choose…</option>
                    {#each groups as g}
                        <option value={g.id}>{g.name}</option>
                    {/each}
                </select>
            </div>
            <div class="form-group">
                <label for="reg-password">Password <span class="hint">at least 8 characters</span></label>
                <div class="pw-row">
                    <input id="reg-password" type={showPassword ? 'text' : 'password'} bind:value={form.password} required minlength="8" maxlength="72" autocomplete="new-password" />
                    <button type="button" class="ghost-btn" on:click={() => (showPassword = !showPassword)} aria-pressed={showPassword}>
                        {showPassword ? 'Hide' : 'Show'}
                    </button>
                </div>
                {#if form.password}
                    <span class="strength" class:bad={form.password.length < 8} class:good={form.password.length >= 8}>
                        {form.password.length < 8 ? `${8 - form.password.length} more character${8 - form.password.length === 1 ? '' : 's'} needed` : '✓ Long enough'}
                    </span>
                {/if}
            </div>
            {#if error}<p class="form-error" role="alert">{error}</p>{/if}
            <button class="submit-btn" type="submit" disabled={busy}>{busy ? 'Creating…' : 'Create account'}</button>
        </form>
        <p class="foot">Already have an account? <a href="/login">Log in</a></p>
    <p class="no-account">📦 Just borrowing something? <a href="/?view=borrow">Borrow without an account</a>. An account adds one-tap returns, lab and robot bookings, and notifications.</p>
    {/if}
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

    .hint,
    .optional-tag {
        font-size: 0.8rem;
        font-weight: 400;
        color: var(--ctp-overlay1);
    }

    .form-group select {
        width: 100%;
        min-height: 46px;
        padding: 10px 14px;
        border: 2px solid var(--ctp-surface0);
        border-radius: 8px;
        background: color-mix(in srgb, var(--bg) 60%, transparent);
        color: var(--ctp-text);
        font: inherit;
        box-sizing: border-box;
        -webkit-backdrop-filter: var(--glass-blur);
        backdrop-filter: var(--glass-blur);
    }

    .form-group select:focus {
        border-color: var(--rp-rose);
        outline: none;
        box-shadow: 0 0 0 3px color-mix(in srgb, var(--rp-rose) 25%, transparent);
    }

    .strength {
        display: block;
        margin-top: 6px;
        font-size: 0.82rem;
        color: var(--ctp-subtext0);
    }

    .strength.bad {
        color: var(--danger);
    }

    .strength.good {
        color: var(--rp-foam);
    }

    :root[data-theme='light'] .strength.good {
        color: var(--rp-pine);
    }

    .done {
        text-align: center;
    }

    .done-icon {
        font-size: 2.6rem;
        margin: 0 0 6px;
    }

    .done h2 {
        margin: 0 0 8px;
        color: var(--rp-foam);
    }

    :root[data-theme='light'] .done h2 {
        color: var(--rp-pine);
    }

    .done p {
        color: var(--ctp-subtext0);
        line-height: 1.5;
        margin: 0 0 18px;
    }

    a.submit-btn {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        box-sizing: border-box;
        text-decoration: none;
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
