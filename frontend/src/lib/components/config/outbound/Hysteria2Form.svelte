<script lang="ts">
	import type { TLSConfig, ObfsConfig, ObfsType, DnsServer } from '$lib/types';
	import { t } from 'svelte-i18n';
	import ServerConfig from './ServerConfig.svelte';
	import DomainResolverField from './DomainResolverField.svelte';
	import { BBR_PROFILES } from '$lib/utils/serverInbound';

	interface Props {
		server: string;
		serverPort: number;
		password: string;
		tls: TLSConfig;
		obfs: ObfsConfig | undefined;
		serverPorts: string;
		hopInterval: string;
		upMbps: number;
		downMbps: number;
		bbrProfile: string;
		domainResolver: string;
		dnsServers: DnsServer[];
		hasDefaultResolver: boolean;
		errors?: Record<string, string>;
		onImport?: () => void;
	}

	let {
		server = $bindable(),
		serverPort = $bindable(),
		password = $bindable(),
		tls = $bindable(),
		obfs = $bindable(),
		serverPorts = $bindable(),
		hopInterval = $bindable(),
		upMbps = $bindable(),
		downMbps = $bindable(),
		bbrProfile = $bindable(''),
		domainResolver = $bindable(''),
		dnsServers = [],
		hasDefaultResolver = false,
		errors = {},
		onImport
	}: Props = $props();

	// Congestion control (#59): the two rates above ARE the switch, and they are
	// two separate switches — Download is announced to the server and decides how
	// the SERVER sends, Upload decides how this client sends (sing-quic
	// client.go: sendBPS of 0 collapses actualTx to 0, so an empty Upload is
	// always BBR outbound, whatever Download says). The profile below applies to
	// whichever half ends up on BBR; '' leaves the fork's default (standard).

	// The obfs block edits the bound `obfs` directly. A local mirror initialised
	// once at mount missed the import that lands while the form is open, and its
	// effect then wrote `undefined` back over the imported obfs (#100).
	function setObfsType(type: ObfsType) {
		// Sizes are gecko-only; switching to salamander drops them.
		obfs = { type, password: obfs?.password ?? '' };
	}
	// gecko only; empty = leave hysteria's defaults (512 / 1200) on both ends.
	function setPacketSize(key: 'min_packet_size' | 'max_packet_size', raw: number) {
		if (!obfs) return;
		const next = { ...obfs };
		if (raw > 0) next[key] = raw;
		else delete next[key];
		obfs = next;
	}
</script>

<div class="space-y-4">
	<!-- Import Button -->
	{#if onImport}
		<button
			type="button"
			onclick={onImport}
			class="w-full px-4 py-2 bg-[var(--ctp-surface0)] border border-dashed border-[var(--ctp-surface2)] rounded-lg text-[var(--ctp-subtext1)] hover:border-[var(--ctp-primary)] hover:text-[var(--ctp-primary)] transition-colors flex items-center justify-center gap-2"
		>
			<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
			</svg>
			{$t('outbounds.importFromHy2')}
		</button>
	{/if}

	<!-- Server & Port -->
	<ServerConfig bind:server bind:serverPort {errors} />

	<!-- Domain Resolver -->
	<DomainResolverField
		bind:value={domainResolver}
		serverAddress={server}
		{dnsServers}
		{hasDefaultResolver}
	/>

	<!-- Password -->
	<div>
		<label for="hy2-password" class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">
			{$t('outbounds.password')} *
		</label>
		<input
			id="hy2-password"
			type="password"
			bind:value={password}
			placeholder="password"
			class="w-full px-3 py-2 bg-[var(--ctp-surface0)] border rounded-lg text-[var(--ctp-text)] placeholder-[var(--ctp-overlay0)] focus:outline-none focus:ring-2 focus:ring-[var(--ctp-primary)] {errors['password'] ? 'border-[var(--ctp-red)]' : 'border-[var(--ctp-surface2)]'}"
		/>
		{#if errors['password']}
			<p class="mt-1 text-sm text-[var(--ctp-red)]">{errors['password']}</p>
		{/if}
	</div>

	<!-- TLS Settings -->
	<div class="bg-[var(--ctp-surface0)] rounded-lg p-4 space-y-4">
		<h3 class="text-sm font-medium text-[var(--ctp-subtext1)]">TLS</h3>

		<!-- No fingerprint picker: QUIC has no uTLS in the binary (see OutboundForm). -->
		<div>
			<label for="hy2-sni" class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">
				{$t('outbounds.sni')}
			</label>
			<input
				id="hy2-sni"
				type="text"
				bind:value={tls.server_name}
				placeholder="example.com"
				class="w-full px-3 py-2 bg-[var(--ctp-mantle)] border border-[var(--ctp-surface2)] rounded-lg text-[var(--ctp-text)] placeholder-[var(--ctp-overlay0)] focus:outline-none focus:ring-2 focus:ring-[var(--ctp-primary)]"
			/>
		</div>

		<label class="flex items-center gap-2 text-sm text-[var(--ctp-subtext1)]">
			<input
				type="checkbox"
				bind:checked={tls.insecure}
				class="w-4 h-4 rounded border-[var(--ctp-surface2)] text-[var(--ctp-primary)] focus:ring-[var(--ctp-primary)]"
			/>
			{$t('outbounds.skipCertVerification')}
		</label>
	</div>

	<!-- Port Hopping -->
	<div class="bg-[var(--ctp-surface0)] rounded-lg p-4 space-y-4">
		<h3 class="text-sm font-medium text-[var(--ctp-subtext1)]">{$t('outbounds.portHopping')}</h3>

		<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
			<div>
				<label for="hy2-ports" class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">
					{$t('outbounds.serverPorts')}
				</label>
				<input
					id="hy2-ports"
					type="text"
					bind:value={serverPorts}
					placeholder="1000-2000,3000-4000"
					class="w-full px-3 py-2 bg-[var(--ctp-mantle)] border border-[var(--ctp-surface2)] rounded-lg text-[var(--ctp-text)] placeholder-[var(--ctp-overlay0)] focus:outline-none focus:ring-2 focus:ring-[var(--ctp-primary)]"
				/>
				<p class="mt-1 text-xs text-[var(--ctp-overlay0)]">{$t('outbounds.portRangesHint')}</p>
			</div>
			<div>
				<label for="hy2-hop" class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">
					{$t('outbounds.hopInterval')}
				</label>
				<input
					id="hy2-hop"
					type="text"
					bind:value={hopInterval}
					placeholder="30s"
					class="w-full px-3 py-2 bg-[var(--ctp-mantle)] border border-[var(--ctp-surface2)] rounded-lg text-[var(--ctp-text)] placeholder-[var(--ctp-overlay0)] focus:outline-none focus:ring-2 focus:ring-[var(--ctp-primary)]"
				/>
			</div>
		</div>
	</div>

	<!-- Bandwidth Limits -->
	<div class="bg-[var(--ctp-surface0)] rounded-lg p-4 space-y-4">
		<h3 class="text-sm font-medium text-[var(--ctp-subtext1)]">{$t('outbounds.bandwidthLimits')}</h3>

		<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
			<div>
				<label for="hy2-up" class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">
					{$t('outbounds.uploadMbps')}
				</label>
				<input
					id="hy2-up"
					type="number"
					bind:value={upMbps}
					min="0"
					placeholder="0"
					class="w-full px-3 py-2 bg-[var(--ctp-mantle)] border border-[var(--ctp-surface2)] rounded-lg text-[var(--ctp-text)] placeholder-[var(--ctp-overlay0)] focus:outline-none focus:ring-2 focus:ring-[var(--ctp-primary)]"
				/>
			</div>
			<div>
				<label for="hy2-down" class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">
					{$t('outbounds.downloadMbps')}
				</label>
				<input
					id="hy2-down"
					type="number"
					bind:value={downMbps}
					min="0"
					placeholder="0"
					class="w-full px-3 py-2 bg-[var(--ctp-mantle)] border border-[var(--ctp-surface2)] rounded-lg text-[var(--ctp-text)] placeholder-[var(--ctp-overlay0)] focus:outline-none focus:ring-2 focus:ring-[var(--ctp-primary)]"
				/>
			</div>
		</div>

		<p class="text-xs text-[var(--ctp-overlay0)]">{$t('outbounds.bandwidthHint')}</p>

		<div>
			<span class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">{$t('outbounds.bbrProfile')}</span>
			<div class="flex gap-2 flex-wrap" role="group" aria-label={$t('outbounds.bbrProfile')}>
				<button type="button" class="toggle-btn {bbrProfile === '' ? 'selected' : ''}"
					onclick={() => (bbrProfile = '')}>{$t('common.default')}</button>
				{#each BBR_PROFILES as p (p)}
					<button type="button" class="toggle-btn {bbrProfile === p ? 'selected' : ''}"
						onclick={() => (bbrProfile = p)}>{$t(`inbounds.server.bbr.${p}`)}</button>
				{/each}
			</div>
			<p class="mt-1 text-xs text-[var(--ctp-overlay0)]">{$t('outbounds.bbrProfileHint')}</p>
		</div>
	</div>

	<!-- Obfuscation -->
	<div class="bg-[var(--ctp-surface0)] rounded-lg p-4 space-y-4">
		<label class="flex items-center gap-2 text-sm font-medium text-[var(--ctp-subtext1)]">
			<input
				type="checkbox"
				checked={!!obfs}
				onchange={(e) => (obfs = e.currentTarget.checked ? { type: 'salamander', password: '' } : undefined)}
				class="w-4 h-4 rounded border-[var(--ctp-surface2)] text-[var(--ctp-primary)] focus:ring-[var(--ctp-primary)]"
			/>
			{$t('outbounds.obfuscationType')}
		</label>

		{#if obfs}
			<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
				<div>
					<span class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">
						{$t('common.type')}
					</span>
					<div class="flex gap-2" role="group" aria-label={$t('common.type')}>
						<button type="button" class="toggle-btn {obfs.type === 'salamander' ? 'selected' : ''}"
							onclick={() => setObfsType('salamander')}>Salamander</button>
						<button type="button" class="toggle-btn {obfs.type === 'gecko' ? 'selected' : ''}"
							onclick={() => setObfsType('gecko')}>Gecko</button>
					</div>
				</div>
				<div>
					<label for="hy2-obfs-pw" class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">
						{$t('outbounds.obfuscationPassword')}
					</label>
					<input
						id="hy2-obfs-pw"
						type="password"
						bind:value={obfs.password}
						placeholder="password"
						class="w-full px-3 py-2 bg-[var(--ctp-mantle)] border border-[var(--ctp-surface2)] rounded-lg text-[var(--ctp-text)] placeholder-[var(--ctp-overlay0)] focus:outline-none focus:ring-2 focus:ring-[var(--ctp-primary)]"
					/>
				</div>
			</div>

			{#if obfs.type === 'gecko'}
				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<div>
						<label for="hy2-obfs-min" class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">
							{$t('inbounds.server.obfsMinPacketSize')}
						</label>
						<input
							id="hy2-obfs-min"
							type="number"
							min="0"
							value={obfs.min_packet_size ?? ''}
							oninput={(e) => setPacketSize('min_packet_size', e.currentTarget.valueAsNumber)}
							class="w-full px-3 py-2 bg-[var(--ctp-mantle)] border border-[var(--ctp-surface2)] rounded-lg text-[var(--ctp-text)] focus:outline-none focus:ring-2 focus:ring-[var(--ctp-primary)]"
						/>
					</div>
					<div>
						<label for="hy2-obfs-max" class="block text-sm font-medium text-[var(--ctp-subtext1)] mb-1">
							{$t('inbounds.server.obfsMaxPacketSize')}
						</label>
						<input
							id="hy2-obfs-max"
							type="number"
							min="0"
							value={obfs.max_packet_size ?? ''}
							oninput={(e) => setPacketSize('max_packet_size', e.currentTarget.valueAsNumber)}
							class="w-full px-3 py-2 bg-[var(--ctp-mantle)] border border-[var(--ctp-surface2)] rounded-lg text-[var(--ctp-text)] focus:outline-none focus:ring-2 focus:ring-[var(--ctp-primary)]"
						/>
					</div>
				</div>
				<!-- The server's own hint says "must match on the client", which reads
				     backwards here (#48) — this is the same sentence from the client side. -->
				<p class="text-xs text-[var(--ctp-overlay0)]">{$t('outbounds.obfsPacketSizeHint')}</p>
			{/if}
		{/if}
	</div>
</div>
