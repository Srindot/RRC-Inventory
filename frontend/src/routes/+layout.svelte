<script lang="ts">
	import Shell from '$lib/Shell.svelte';
	import '$lib/theme.css';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href="/rrc_logo.png" />
</svelte:head>

<Shell>{@render children?.()}</Shell>
