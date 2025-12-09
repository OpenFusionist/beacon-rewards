const appRoot = document.getElementById('app-root');
if (!appRoot) {
    throw new Error('App root not found');
}

const navLinks = Array.from(document.querySelectorAll('.nav-links a'));
let renderEpoch = 0;
let cleanupCurrentView = () => { };
let networkChart = null;

const routes = [
    { name: 'topWithdrawals', match: (path) => path.startsWith('/deposits/top-withdrawals'), render: renderTopWithdrawals },
    { name: 'networkRewards', match: (path) => path.startsWith('/rewards/network'), render: renderNetworkRewards },
    { name: 'addressRewards', match: (path) => path.startsWith('/rewards/by-address'), render: renderAddressRewards },
];

function createViewCleanup() {
    const listeners = [];
    return {
        add(target, event, handler, options) {
            target.addEventListener(event, handler, options);
            listeners.push(() => target.removeEventListener(event, handler, options));
        },
        cleanup() {
            while (listeners.length) {
                const off = listeners.pop();
                off();
            }
        }
    };
}

function formatGweiToAce(gwei) {
    const num = Number(gwei);
    if (Number.isNaN(num)) return '0';
    return (num / 1e9).toFixed(6);
}

function formatNumber(num) {
    const parsed = typeof num === 'string' ? Number(num) : num;
    if (Number.isNaN(parsed)) {
        return '0';
    }
    return new Intl.NumberFormat('en-US').format(parsed);
}

const utcDateFormatter = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'UTC',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
});

const utcTimeFormatter = new Intl.DateTimeFormat('en-GB', {
    timeZone: 'UTC',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
});

function formatTime(value) {
    if (!value) return '';
    const date = new Date(value);
    return `${utcDateFormatter.format(date)} ${utcTimeFormatter.format(date)} UTC`;
}

function formatDuration(seconds) {
    const secs = Number(seconds) || 0;
    const days = Math.floor(secs / 86400);
    const hours = Math.floor((secs % 86400) / 3600);
    const minutes = Math.floor((secs % 3600) / 60);
    if (days > 0) {
        return `${days}d ${hours}h`;
    }
    if (hours > 0) {
        return `${hours}h${minutes > 0 ? ` ${minutes}m` : ''}`;
    }
    return `${minutes}m`;
}

function cssVar(name, fallback = '') {
    const value = getComputedStyle(document.documentElement).getPropertyValue(name);
    const trimmed = value ? value.trim() : '';
    return trimmed || fallback;
}

function hexToRgb(value) {
    if (!value) return '';
    const hex = value.trim().replace('#', '');
    const normalized = hex.length === 3 ? hex.split('').map((ch) => ch + ch).join('') : hex;
    if (normalized.length !== 6) {
        return '';
    }
    const r = parseInt(normalized.slice(0, 2), 16);
    const g = parseInt(normalized.slice(2, 4), 16);
    const b = parseInt(normalized.slice(4, 6), 16);
    if ([r, g, b].some((channel) => Number.isNaN(channel))) {
        return '';
    }
    return `${r}, ${g}, ${b}`;
}

function formatAddress(addr) {
    if (!addr) return '';
    if (addr.length > 12) {
        return `${addr.slice(0, 6)}...${addr.slice(-4)}`;
    }
    return addr;
}

async function copyText(text) {
    if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
        return navigator.clipboard.writeText(text);
    }
    return new Promise((resolve, reject) => {
        const textarea = document.createElement('textarea');
        textarea.value = text;
        textarea.style.position = 'fixed';
        textarea.style.opacity = '0';
        document.body.appendChild(textarea);
        textarea.focus();
        textarea.select();
        try {
            const ok = document.execCommand('copy');
            ok ? resolve() : reject(new Error('execCommand copy failed'));
        } catch (err) {
            reject(err);
        } finally {
            document.body.removeChild(textarea);
        }
    });
}

const toast = (() => {
    let timer = null;
    let el = null;

    function ensure() {
        if (!el) {
            el = document.createElement('div');
            el.className = 'copy-toast';
            document.body.appendChild(el);
        }
        return el;
    }

    return {
        show(message) {
            const node = ensure();
            node.textContent = message;
            node.classList.add('show');
            clearTimeout(timer);
            timer = setTimeout(() => node.classList.remove('show'), 1600);
        }
    };
})();

function setNavActive(pathname) {
    navLinks.forEach((link) => {
        const linkPath = new URL(link.href, window.location.origin).pathname;
        if (pathname.startsWith(linkPath)) {
            link.classList.add('active');
        } else {
            link.classList.remove('active');
        }
    });
}

function routeFor(pathname) {
    return routes.find((route) => route.match(pathname));
}

function renderError(message) {
    return `<div class="error">${message}</div>`;
}

function footnotesTopWithdrawals() {
    return '';
}

function footnotesNetwork() {
    return `
        <div class="footnotes">
            <h3>Field reference</h3>
            <ol>
                <li><strong>Window</strong>: Reporting window used (start to end)</li>
                <li><strong>Active validator count</strong>: Total validators currently active</li>
                <li><strong>CL rewards</strong>: Consensus layer rewards (ACE)</li>
                <li><strong>EL rewards</strong>: Execution layer rewards (ACE)</li>
                <li><strong>Total rewards</strong>: Sum of CL and EL rewards (ACE)</li>
                <li><strong>Total effective balance</strong>: Effective balance across all active validators (ACE)</li>
                <li><strong>Projected APR</strong>: Annualized rate based on the current window rewards (%)</li>
            </ol>
        </div>
    `;
}

function footnotesAddress() {
    return `
        <div class="footnotes">
            <h3>Field reference</h3>
            <ol>
                <li><strong>Address</strong>: Deposit or withdrawal address being queried</li>
                <li><strong>Active validators</strong>: Number of validators funded by this address that are currently active</li>
                <li><strong>Validator indices</strong>: Index numbers of all active validators funded by this address (optional)</li>
                <li><strong>CL rewards</strong>: Consensus layer rewards (ACE)</li>
                <li><strong>EL rewards</strong>: Execution layer rewards (ACE)</li>
                <li><strong>Total rewards</strong>: Sum of CL and EL rewards (ACE)</li>
                <li><strong>Estimated 31d rewards</strong>: Estimated rewards for the past 31 days based on network APR (ACE)</li>
                <li><strong>Total effective balance</strong>: Sum of effective balances for all active validators (ACE)</li>
                <li><strong>Weighted average stake time</strong>: Active validators average staking duration weighted by effective balance</li>
                <li><strong>Window</strong>: Reporting window used for the calculation</li>
            </ol>
        </div>
    `;
}

function shouldHandleLink(event, link) {
    const isMiddleClick = event.button === 1 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey;
    const target = link.getAttribute('target');
    const isExternal = link.host !== window.location.host || target === '_blank';
    return !isMiddleClick && !isExternal;
}

async function navigate(urlString, options = {}) {
    const url = new URL(urlString, window.location.origin);
    const route = routeFor(url.pathname);
    if (!route) {
        window.location.href = url.toString();
        return;
    }

    if (!options.skipHistory) {
        if (options.replace) {
            history.replaceState({}, '', url);
        } else {
            history.pushState({}, '', url);
        }
    }

    setNavActive(url.pathname);

    const cleaner = createViewCleanup();
    cleanupCurrentView();
    cleanupCurrentView = cleaner.cleanup;

    const ticket = ++renderEpoch;
    let teardown;
    try {
        teardown = await route.render({ url, ticket, cleaner });
    } catch (err) {
        console.error('Failed to render route', err);
        appRoot.innerHTML = renderError('Render failed, please retry.');
    }
    if (typeof teardown === 'function') {
        cleanupCurrentView = () => {
            teardown();
            cleaner.cleanup();
        };
    } else {
        cleanupCurrentView = cleaner.cleanup;
    }
}

function runCopyHandler() {
    appRoot.addEventListener('click', (e) => {
        const target = e.target.closest('.address-copy-target');
        if (!target) return;
        const address = target.dataset.address;
        if (!address) return;

        copyText(address)
            .then(() => toast.show('Copied'))
            .catch(() => toast.show('Copy failed'));
    });
}

function topWithdrawalsTemplate() {
    return `
        <div id="top-withdrawals-view" class="page-shell" data-view="top-withdrawals">
            <section class="page-header">
                <div>
                    <div class="page-eyebrow">Validator Leaderboard</div>
                    <h1 class="page-title">Top staking addresses</h1>
                    <p class="page-description">
                        Aggregates validators by withdrawal address with deposit totals, activity, and effective balance. Click any header to change the sort.
                    </p>
                    <div class="meta-row">
                        <span class="meta-pill" id="top-withdrawals-summary">Loading leaderboard</span>
                        <span class="meta-pill" id="top-withdrawals-sort">Sort: -</span>
                        <span class="meta-pill">Unit: ACE</span>
                        <span class="meta-pill">Tip: Click headers to sort</span>
                    </div>
                </div>
            </section>

            <section class="table-card">
                <div class="table-toolbar">
                    <div>
                        <div class="table-title">Staking leaderboard</div>
                        <div class="table-subtitle">Indexes validators by withdrawal address with totals and status</div>
                    </div>
                    <div class="table-note" id="top-withdrawals-note">Loading...</div>
                </div>
                <div id="top-withdrawals-table">
                    <div class="loading">Loading...</div>
                </div>
            </section>
        </div>
        ${footnotesTopWithdrawals()}
    `;
}

async function renderTopWithdrawals({ url, ticket, cleaner }) {
    const params = url.searchParams;
    const state = {
        limit: Math.max(1, Number(params.get('limit')) || 30),
        sortBy: params.get('sort_by') || 'total_active_effective_balance',
        order: 'desc',
    };

    const sortLabels = {
        total_active_effective_balance: 'Total effective balance',
        total_deposit: 'Total deposit',
        withdrawal_address: 'Withdrawal address',
        validators_total: 'Validators funded',
        active: 'Active validators',
        slashed: 'Slashed validators',
        voluntary_exited: 'Voluntary exits',
    };

    const resolveSortLabel = (key) => sortLabels[key] || key;

    appRoot.innerHTML = topWithdrawalsTemplate();
    const tableContainer = appRoot.querySelector('#top-withdrawals-table');
    const summary = appRoot.querySelector('#top-withdrawals-summary');
    const sortMeta = appRoot.querySelector('#top-withdrawals-sort');
    const note = appRoot.querySelector('#top-withdrawals-note');

    const setQueryParams = () => {
        const nextUrl = new URL(url.toString());
        nextUrl.searchParams.set('limit', state.limit);
        nextUrl.searchParams.set('sort_by', state.sortBy);
        nextUrl.searchParams.set('order', state.order);
        history.replaceState({}, '', nextUrl);
        url = nextUrl;
    };

    const updateSortIndicators = () => {
        appRoot.querySelectorAll('th[data-sort]').forEach((th) => {
            th.classList.remove('sort-asc', 'sort-desc');
            const sortKey = th.getAttribute('data-sort');
            if (sortKey === state.sortBy) {
                th.classList.add(state.order === 'asc' ? 'sort-asc' : 'sort-desc');
            }
        });
    };

    const renderTable = (results) => {
        if (!Array.isArray(results) || results.length === 0) {
            tableContainer.innerHTML = '<div class="empty">No data available</div>';
            return;
        }

        const rows = results.map((item, index) => {
            const rankClass = index === 0 ? 'rank top-1' : index === 1 ? 'rank top-2' : index === 2 ? 'rank top-3' : 'rank';
            return `
                <tr>
                    <td><span class="${rankClass}">${index + 1}</span></td>
                    <td>
                        ${item.withdrawal_address ? `
                            <div class="address-with-copy address-copy-target" data-address="${item.withdrawal_address}" title="${item.withdrawal_address}">
                                <span class="address">${formatAddress(item.withdrawal_address)}</span>
                            </div>
                        ` : '-'}
                    </td>
                    <td>${item.label || '-'}</td>
                    <td>${formatGweiToAce(item.total_active_effective_balance)}</td>
                    <td>${formatGweiToAce(item.total_deposit)}</td>
                    <td>${formatNumber(item.validators_total)}</td>
                    <td><span class="badge badge-success">${formatNumber(item.active)}</span></td>
                    <td>${Number(item.slashed) > 0 ? `<span class="badge badge-danger">${formatNumber(item.slashed)}</span>` : '0'}</td>
                    <td>${Number(item.voluntary_exited) > 0 ? `<span class="badge badge-warning">${formatNumber(item.voluntary_exited)}</span>` : '0'}</td>
                </tr>
            `;
        }).join('');

        tableContainer.innerHTML = `
            <div class="table-container">
                <table>
                    <colgroup>
                    <col class="col-rank">
                    <col class="col-withdrawal">
                    <col class="col-label">
                    <col class="col-effective-balance">
                    <col class="col-total-deposit">
                    <col class="col-validators">
                    <col class="col-active">
                    <col class="col-slashed">
                    <col class="col-voluntary">
                </colgroup>
            <thead>
                <tr>
                    <th><abbr class="header-abbr" data-tooltip="Ranking" aria-label="Ranking position">Rank</abbr></th>
                    <th><abbr class="header-abbr" data-tooltip="Withdrawal address" aria-label="Withdrawal address">Wdr</abbr></th>
                    <th><abbr class="header-abbr" data-tooltip="Label for the withdrawal address" aria-label="Label for the withdrawal address">Label</abbr></th>
                    <th class="sortable" data-sort="total_active_effective_balance"><abbr class="header-abbr" data-tooltip="Total effective balance (ACE)" aria-label="Total effective balance (ACE)">EB</abbr></th>
                    <th class="sortable" data-sort="total_deposit"><abbr class="header-abbr" data-tooltip="Total deposit amount (ACE)" aria-label="Total deposit amount (ACE)">Dep</abbr></th>
                    <th class="sortable" data-sort="validators_total"><abbr class="header-abbr" data-tooltip="Total Number of validators" aria-label="Total Number of validators">Tot</abbr></th>
                    <th class="sortable" data-sort="active"><abbr class="header-abbr" data-tooltip="Active validators" aria-label="Active validators">Act</abbr></th>
                    <th class="sortable" data-sort="slashed"><abbr class="header-abbr" data-tooltip="Number of slashed validators" aria-label="Number of slashed validators">Sla</abbr></th>
                    <th class="sortable" data-sort="voluntary_exited"><abbr class="header-abbr" data-tooltip="Number of voluntarily exited validators" aria-label="Number of voluntarily exited validators">Ex</abbr></th>
                </tr>
            </thead>
            <tbody>${rows}</tbody>
            </table>
        </div>
        `;
        updateSortIndicators();
    };

    const fetchTable = async () => {
        summary.textContent = 'Loading leaderboard...';
        sortMeta.textContent = 'Sort: -';
        note.textContent = 'Fetching the latest results';
        tableContainer.innerHTML = '<div class="loading">Loading...</div>';
        let response;
        try {
            response = await fetch(`/deposits/top-withdrawals?limit=${state.limit}&sort_by=${state.sortBy}&order=${state.order}`, {
                headers: { Accept: 'application/json' },
            });
        } catch (err) {
            if (ticket === renderEpoch) {
                tableContainer.innerHTML = renderError('Failed to load, please try again.');
                summary.textContent = 'Load failed';
                sortMeta.textContent = 'Sort: -';
                note.textContent = 'Request failed, please try again.';
            }
            return;
        }

        let payload;
        try {
            payload = await response.json();
        } catch (err) {
            payload = null;
        }

        if (ticket !== renderEpoch) {
            return;
        }

        if (!response.ok || !payload) {
            tableContainer.innerHTML = renderError((payload && payload.error) || 'Failed to load');
            summary.textContent = 'Load failed';
            sortMeta.textContent = 'Sort: -';
            note.textContent = 'Request failed, please try again.';
            return;
        }

        state.limit = payload.limit || state.limit;
        state.sortBy = payload.sort_by || state.sortBy;
        state.order = 'desc';

        const sortLabel = resolveSortLabel(state.sortBy);
        const orderLabel = state.order === 'asc' ? 'ascending' : 'descending';
        summary.textContent = `Top ${state.limit} · ${sortLabel}`;
        sortMeta.textContent = `Sort: ${sortLabel} (${orderLabel})`;
        note.textContent = `Current: Top ${state.limit} · ${sortLabel} · ${orderLabel}`;
        renderTable(payload.results || []);
        setQueryParams();
    };

    const handleHeaderClick = (event) => {
        const th = event.target.closest('th.sortable');
        if (!th) return;
        const sortKey = th.getAttribute('data-sort');
        if (!sortKey) return;

        if (sortKey !== state.sortBy) {
            state.sortBy = sortKey;
        }
        state.order = 'desc';
        fetchTable();
    };

    cleaner.add(appRoot, 'click', handleHeaderClick);
    await fetchTable();
}

function networkTemplate() {
    return `
        <div id="network-rewards-view" class="network-shell">
            <div class="loading">Loading...</div>
        </div>
        ${footnotesNetwork()}
    `;
}

async function renderNetworkRewards({ ticket }) {
    appRoot.innerHTML = networkTemplate();
    const container = appRoot.querySelector('#network-rewards-view');

    container.innerHTML = '<div class="loading">Loading...</div>';
    let response;
    try {
        response = await fetch('/rewards/network', { headers: { Accept: 'application/json' } });
    } catch (err) {
        if (ticket === renderEpoch) {
            container.innerHTML = renderError('Failed to load, please try again.');
        }
        return () => destroyChart();
    }

    let payload;
    try {
        payload = await response.json();
    } catch (err) {
        payload = null;
    }

    if (ticket !== renderEpoch) {
        return () => destroyChart();
    }

    if (!response.ok || !payload || !payload.current) {
        container.innerHTML = renderError((payload && payload.error) || 'Unable to fetch network rewards right now');
        return () => destroyChart();
    }

    container.innerHTML = renderNetworkStats(payload.current, payload.history || []);
    if (Array.isArray(payload.history) && payload.history.length > 0) {
        renderHistoryChart(payload.history);
    } else {
        destroyChart();
    }

    return () => destroyChart();
}

function renderNetworkStats(current, history) {
    const historyBlock = history.length
        ? `
            <section class="chart-panel">
                <div class="panel-header">
                    <h2 class="panel-title">Reward history</h2>
                    <p class="panel-note">Last ${history.length} windows</p>
                </div>
                <div class="chart-container">
                    <canvas id="rewardsChart"></canvas>
                </div>
            </section>
        `
        : '';

    return `
        <section class="network-header">
            <div class="header-copy">
                <div class="network-eyebrow">Network Rewards</div>
                <h1 class="network-title">Network rewards</h1>
                <p class="network-description">
                    Summarizes the latest reward window across the consensus and execution layers.
                </p>
                <div class="network-meta">
                    <div class="meta-item">
                        <span class="meta-label">Window start</span>
                        <span class="meta-value">${formatTime(current.window_start)}</span>
                    </div>
                    <div class="meta-item">
                        <span class="meta-label">Window end</span>
                        <span class="meta-value">${formatTime(current.window_end)}</span>
                    </div>
                    <div class="meta-item">
                        <span class="meta-label">Duration</span>
                        <span class="meta-value">${formatDuration(current.window_duration_seconds)}</span>
                    </div>
                </div>
            </div>
            <div class="window-highlight">
                <div class="window-label">Active validators</div>
                <div class="window-value">${formatNumber(current.active_validator_count)}</div>
                <div class="window-label">Total effective balance</div>
                <div class="window-subvalue">${formatNumber(formatGweiToAce(current.total_effective_balance_gwei))} ACE</div>
            </div>
        </section>

        <section class="metrics-grid">
            <article class="metric-card accent">
                <div class="metric-label">Projected APR</div>
                <div class="metric-value">${Number(current.project_apr_percent || 0).toFixed(3)}%</div>
                <div class="metric-subtext">Based on the latest reward window</div>
            </article>
            <article class="metric-card">
                <div class="metric-label">Total rewards (ACE)</div>
                <div class="metric-value">${formatNumber(formatGweiToAce(current.total_rewards_gwei))}</div>
                <div class="metric-foot">
                    <span class="pill"><span class="dot"></span>CL: ${formatNumber(formatGweiToAce(current.cl_rewards_gwei))}</span>
                    <span class="pill" data-variant="warning"><span class="dot"></span>EL: ${formatNumber(formatGweiToAce(current.el_rewards_gwei))}</span>
                </div>
            </article>
            <article class="metric-card">
                <div class="metric-label">Consensus rewards (ACE)</div>
                <div class="metric-value">${formatNumber(formatGweiToAce(current.cl_rewards_gwei))}</div>
            </article>
            <article class="metric-card">
                <div class="metric-label">Execution rewards (ACE)</div>
                <div class="metric-value">${formatNumber(formatGweiToAce(current.el_rewards_gwei))}</div>
            </article>
        </section>

        ${historyBlock}
    `;
}

function renderHistoryChart(history) {
    const canvas = document.getElementById('rewardsChart');
    if (!canvas || typeof Chart === 'undefined') {
        return;
    }

    const sorted = [...history].sort((a, b) => new Date(a.window_start) - new Date(b.window_start));
    const labels = sorted.map((h) => new Date(h.window_end).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }));
    const totalRewards = sorted.map((h) => Number(h.total_rewards_gwei) / 1e9);
    const aprData = sorted.map((h) => Number(h.project_apr_percent));

    const primary = cssVar('--color-primary') || cssVar('--primary-color');
    const warning = cssVar('--color-warning') || cssVar('--warning-color');
    const primaryRgb = cssVar('--color-primary-rgb') || hexToRgb(primary);
    const warningRgb = cssVar('--color-warning-rgb') || hexToRgb(warning);

    const palette = {
        primary,
        primaryFill: primaryRgb ? `rgba(${primaryRgb}, 0.14)` : primary,
        warning,
        warningFill: warningRgb ? `rgba(${warningRgb}, 0.16)` : warning,
        grid: cssVar('--color-border') || cssVar('--border-color'),
        text: cssVar('--text-secondary') || cssVar('--color-text-muted'),
        background: cssVar('--card-bg') || cssVar('--color-surface'),
    };

    destroyChart();
    const ctx = canvas.getContext('2d');
    networkChart = new Chart(ctx, {
        type: 'line',
        data: {
            labels,
            datasets: [
                {
                    label: 'Total rewards (ACE)',
                    data: totalRewards,
                    borderColor: palette.primary,
                    backgroundColor: palette.primaryFill,
                    yAxisID: 'y',
                    tension: 0.2,
                    pointRadius: 3,
                },
                {
                    label: 'Projected APR (%)',
                    data: aprData,
                    borderColor: palette.warning,
                    backgroundColor: palette.warningFill,
                    yAxisID: 'y1',
                    tension: 0.2,
                    pointRadius: 3,
                },
            ],
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: {
                mode: 'index',
                intersect: false,
            },
            plugins: {
                legend: { position: 'bottom', labels: { color: palette.text, usePointStyle: true } },
                tooltip: {
                    backgroundColor: palette.background,
                    titleColor: palette.text,
                    bodyColor: palette.text,
                    callbacks: {
                        label(context) {
                            if (context.datasetIndex === 0) {
                                return `Total rewards: ${formatNumber(context.parsed.y)} ACE`;
                            }
                            return `Projected APR: ${context.parsed.y.toFixed(3)}%`;
                        },
                    },
                },
            },
            scales: {
                x: {
                    ticks: { color: palette.text },
                    grid: { color: palette.grid, drawBorder: false },
                },
                y: {
                    type: 'linear',
                    display: true,
                    position: 'left',
                    title: { display: true, text: 'Total rewards (ACE)', color: palette.text },
                    ticks: { color: palette.text },
                    grid: { color: palette.grid, drawBorder: false },
                },
                y1: {
                    type: 'linear',
                    display: true,
                    position: 'right',
                    title: { display: true, text: 'Projected APR (%)', color: palette.text },
                    grid: { drawOnChartArea: false, color: palette.grid },
                    ticks: { color: palette.text },
                },
            },
        },
    });
}

function destroyChart() {
    if (networkChart) {
        networkChart.destroy();
        networkChart = null;
    }
}

function addressTemplate() {
    return `
        <div class="card">
            <div class="card-header">
                <h1 class="card-title">Address rewards lookup</h1>
            </div>

            <form id="query-form">
                <div class="form-group">
                    <label for="address">Address or withdrawal credentials</label>
                    <input type="text" 
                           id="address" 
                           name="address" 
                           placeholder="0x..." 
                           required
                           pattern="0x[a-fA-F0-9]{40}|0x0[12][a-fA-F0-9]{62}">
                    <small style="color: var(--text-secondary); display: block; margin-top: var(--space-1);">
                        Supports standard addresses (0x + 40 chars) or withdrawal credentials (0x01/0x02 + 64 chars)
                    </small>
                </div>

                <div class="form-group">
                    <label>
                        <input type="checkbox" name="include_validator_indices" value="true">
                        Include validator indices
                    </label>
                </div>

                <button type="submit" id="submit-btn">Search</button>
                <span id="loading" style="margin-left: var(--space-4); display: none;">Loading...</span>
            </form>
        </div>

        <div id="result-container"></div>
        ${footnotesAddress()}
    `;
}

function renderAddressResult(data) {
    return `
        <div class="card" style="margin-top: var(--space-6);">
            <div class="card-header">
                <h2 class="card-title">Result</h2>
            </div>

            <div class="stats-grid">
                <div class="stat-card">
                    <div class="stat-label">Address</div>
                    <div class="stat-value address address-copy-target" data-address="${data.address}" title="${data.address}" style="font-size: 1rem;">
                        ${formatAddress(data.address)}
                    </div>
                    ${data.depositor_label ? `<div style="margin-top: var(--space-2); color: var(--text-secondary); font-size: 0.875rem;">${data.depositor_label}</div>` : ''}
                </div>

                <div class="stat-card stat-card-accent">
                    <div class="stat-label">Window</div>
                    <div class="stat-value" style="font-size: 0.9rem; line-height: 1.5;">
                        ${formatTime(data.window_start)}<br>
                        to<br>
                        ${formatTime(data.window_end)}
                    </div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">Active validators</div>
                    <div class="stat-value">${formatNumber(data.active_validator_count)}</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">Total effective balance</div>
                    <div class="stat-value">${formatNumber(formatGweiToAce(data.total_effective_balance_gwei))} ACE</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">CL rewards</div>
                    <div class="stat-value">${formatNumber(formatGweiToAce(data.cl_rewards_gwei))} ACE</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">EL rewards</div>
                    <div class="stat-value">${formatNumber(formatGweiToAce(data.el_rewards_gwei))} ACE</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">Total rewards</div>
                    <div class="stat-value">${formatNumber(formatGweiToAce(data.total_rewards_gwei))} ACE</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">Estimated rewards (31 days)</div>
                    <div class="stat-value">${formatNumber(formatGweiToAce(data.estimated_history_rewards_31d_gwei || 0))} ACE</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">Weighted average stake time</div>
                    <div class="stat-value">${formatDuration(data['weighted_average_stake_time(seconds)'])}</div>
                </div>
            </div>

            ${data.validator_indices && data.validator_indices.length > 0 ? `
                <details style="margin-top: var(--space-6);">
                    <summary style="cursor: pointer; font-weight: 600; padding: var(--space-2); background: var(--bg-color); border-radius: var(--radius-md);">
                        Validator indices (${data.validator_indices.length})
                    </summary>
                    <div style="margin-top: var(--space-2); padding: var(--space-4); background: var(--bg-color); border-radius: var(--radius-md); max-height: 300px; overflow-y: auto;">
                        <div style="display: flex; flex-wrap: wrap; gap: var(--space-2);">
                            ${data.validator_indices.map((idx) => `<span class="badge badge-success">${idx}</span>`).join('')}
                        </div>
                    </div>
                </details>
            ` : ''}
        </div>
    `;
}

async function renderAddressRewards({ ticket, cleaner }) {
    appRoot.innerHTML = addressTemplate();
    const form = appRoot.querySelector('#query-form');
    const loading = appRoot.querySelector('#loading');
    const result = appRoot.querySelector('#result-container');

    const setLoading = (visible) => {
        loading.style.display = visible ? 'inline-block' : 'none';
    };

    const handleSubmit = async (event) => {
        event.preventDefault();
        const formData = new FormData(form);
        const address = (formData.get('address') || '').trim();
        const includeIndices = formData.get('include_validator_indices') === 'true';

        if (!address) {
            result.innerHTML = renderError('Address is required');
            return;
        }

        setLoading(true);
        result.innerHTML = '';

        let response;
        try {
            response = await fetch(`/rewards/by-address${includeIndices ? '?include_validator_indices=true' : ''}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
                body: JSON.stringify({ address }),
            });
        } catch (err) {
            if (ticket === renderEpoch) {
                result.innerHTML = renderError('Request failed, please try again.');
                setLoading(false);
            }
            return;
        }

        let payload;
        try {
            payload = await response.json();
        } catch (err) {
            payload = null;
        }

        if (ticket !== renderEpoch) {
            return;
        }

        setLoading(false);

        if (!response.ok || !payload) {
            result.innerHTML = renderError((payload && payload.error) || 'Lookup failed');
            return;
        }

        result.innerHTML = renderAddressResult(payload);
    };

    cleaner.add(form, 'submit', handleSubmit);
}

function initNavigation() {
    navLinks.forEach((link) => {
        link.addEventListener('click', (event) => {
            if (!shouldHandleLink(event, link)) {
                return;
            }
            event.preventDefault();
            navigate(link.href);
        });
    });

    window.addEventListener('popstate', () => {
        navigate(window.location.href, { replace: true, skipHistory: true });
    });

    navigate(window.location.href, { replace: true, skipHistory: true });
    runCopyHandler();
}

initNavigation();
