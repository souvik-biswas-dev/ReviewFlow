<script lang="ts">
	import '../app.css';
	import type { Snippet } from 'svelte';
	import { setContextClient } from '@urql/svelte';
	import { createUrqlClient } from '$lib/urql';

	let { children }: { children: Snippet } = $props();

	// One GraphQL client for the whole app, shared via context.
	setContextClient(createUrqlClient());
</script>

<!-- Ambient aurora backdrop: three slow-drifting blurred orbs. -->
<div class="pointer-events-none fixed inset-0 -z-10 overflow-hidden">
	<div
		class="absolute -left-40 -top-40 h-[40rem] w-[40rem] animate-aurora-drift rounded-full bg-aurora-violet/20 blur-[120px]"
	></div>
	<div
		class="absolute -right-40 top-1/3 h-[36rem] w-[36rem] animate-aurora-drift rounded-full bg-aurora-cyan/15 blur-[120px] [animation-delay:-6s]"
	></div>
	<div
		class="absolute bottom-0 left-1/3 h-[30rem] w-[30rem] animate-aurora-drift rounded-full bg-aurora-pink/10 blur-[120px] [animation-delay:-12s]"
	></div>
</div>

{@render children()}
