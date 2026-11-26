<script lang="ts">
	import '@tabler/core/dist/css/tabler.min.css';
	import '$lib/theme.css';
	import Shell from '$lib/Shell.svelte';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href="/rrc_logo.png" />
</svelte:head>

<Shell>{@render children?.()}</Shell>
