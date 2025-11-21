const appRoot = document.getElementById('app-root');
if (!appRoot) {
    throw new Error('App root not found');
}

const navLinks = Array.from(document.querySelectorAll('.nav-links a'));
let renderEpoch = 0;
let cleanupCurrentView = () => {};
let networkChart = null;

const routes = [
    { name: 'topDeposits', match: (path) => path.startsWith('/deposits/top-deposits'), render: renderTopDeposits },
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
    return new Intl.NumberFormat('zh-CN').format(parsed);
}

function formatTime(value) {
    if (!value) return '';
    return new Date(value).toLocaleString('zh-CN', { timeZone: 'UTC' }) + ' UTC';
}

function formatDuration(seconds) {
    const secs = Number(seconds) || 0;
    const days = Math.floor(secs / 86400);
    const hours = Math.floor((secs % 86400) / 3600);
    const minutes = Math.floor((secs % 3600) / 60);
    if (days > 0) {
        return `${days}天 ${hours}小时`;
    }
    if (hours > 0) {
    return `${hours}小时${minutes > 0 ? minutes + '分钟' : ''}`;
    }
    return `${minutes}分钟`;
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

function footnotesTopDeposits() {
    return `
        <div class="footnotes">
            <h3>字段说明</h3>
            <ol>
                <li><strong>存款地址</strong>：向存款合约发送交易的地址</li>
                <li><strong>总存款金额</strong>：该地址向存款合约发送的总金额（单位：ACE）</li>
                <li><strong>Validator 总数</strong>：该地址资助的验证者总数</li>
                <li><strong>活跃数量</strong>：当前处于活跃状态的验证者数量（未被罚没且有效余额大于0）</li>
                <li><strong>被罚没数量</strong>：因违规行为被罚没的验证者数量</li>
                <li><strong>主动退出数量</strong>：主动退出且有效余额为0的验证者数量</li>
                <li><strong>总有效余额</strong>：所有活跃验证者的有效余额总和（单位：ACE）</li>
            </ol>
        </div>
    `;
}

function footnotesNetwork() {
    return `
        <div class="footnotes">
            <h3>字段说明</h3>
            <ol>
                <li><strong>时间窗口</strong>：当前统计的时间范围（从窗口开始到窗口结束）</li>
                <li><strong>活跃 Validator 数量</strong>：当前处于活跃状态的验证者总数</li>
                <li><strong>CL 收益</strong>：共识层收益（单位：ACE）</li>
                <li><strong>EL 收益</strong>：执行层收益（单位：ACE）</li>
                <li><strong>总收益</strong>：CL 收益和 EL 收益的总和（单位：ACE）</li>
                <li><strong>总有效余额</strong>：所有活跃验证者的有效余额总和（单位：ACE）</li>
                <li><strong>预估 APR</strong>：基于当前窗口收益计算的年化收益率（百分比）</li>
            </ol>
        </div>
    `;
}

function footnotesAddress() {
    return `
        <div class="footnotes">
            <h3>字段说明</h3>
            <ol>
                <li><strong>地址</strong>：查询的存款地址或提款地址</li>
                <li><strong>活跃 Validator 数量</strong>：该地址资助的当前处于活跃状态的验证者数量</li>
                <li><strong>Validator 索引列表</strong>：该地址资助的所有活跃验证者的索引号（可选显示）</li>
                <li><strong>CL 收益</strong>：共识层收益（单位：ACE）</li>
                <li><strong>EL 收益</strong>：执行层收益（单位：ACE）</li>
                <li><strong>总收益</strong>：CL 和 EL 收益的总和（单位：ACE）</li>
                <li><strong>总有效余额</strong>：所有活跃验证者的有效余额总和（单位：ACE）</li>
                <li><strong>加权平均质押时间</strong>：基于存款金额加权的平均质押时长</li>
                <li><strong>时间窗口</strong>：当前统计的时间范围</li>
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
        appRoot.innerHTML = renderError('页面渲染失败，请重试');
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
            .then(() => toast.show('复制成功'))
            .catch(() => toast.show('复制失败'));
    });
}

function topDepositsTemplate() {
    return `
        <div id="top-deposits-view" class="page-shell" data-view="top-deposits">
            <section class="page-header">
                <div>
                    <div class="page-eyebrow">Depositor Leaderboard</div>
                    <h1 class="page-title">大户排行</h1>
                    <p class="page-description">
                        汇总前排存款地址，展示 Validator 规模、活跃状态与总有效余额；点击表头即可切换排序。
                    </p>
                    <div class="meta-row">
                        <span class="meta-pill" id="top-deposits-summary">列表加载中</span>
                        <span class="meta-pill" id="top-deposits-sort">排序：-</span>
                        <span class="meta-pill">单位：ACE</span>
                        <span class="meta-pill">交互：点击表头切换</span>
                    </div>
                </div>
                <div class="summary-card">
                    <div class="summary-label">数据刷新</div>
                    <div class="summary-value" id="top-deposits-window">实时查询</div>
                    <div class="summary-subtext">按指定排序字段返回 Top N 结果</div>
                </div>
            </section>

            <section class="table-card">
                <div class="table-toolbar">
                    <div>
                        <div class="table-title">存款地址排行榜</div>
                        <div class="table-subtitle">包含存款额、Validator 状态和有效余额</div>
                    </div>
                    <div class="table-note" id="top-deposits-note">加载中...</div>
                </div>
                <div id="top-deposits-table">
                    <div class="loading">加载中...</div>
                </div>
            </section>
        </div>
        ${footnotesTopDeposits()}
    `;
}

async function renderTopDeposits({ url, ticket, cleaner }) {
    const params = url.searchParams;
    const state = {
        limit: Math.max(1, Number(params.get('limit')) || 100),
        sortBy: params.get('sort_by') || 'total_deposit',
        order: params.get('order') === 'asc' ? 'asc' : 'desc',
    };

    const sortLabels = {
        total_deposit: '总存款金额',
        validators_total: 'Validator 总数',
        active: '活跃数量',
        slashed: '被罚没数量',
        voluntary_exited: '主动退出数量',
        total_active_effective_balance: '总有效余额',
    };

    const resolveSortLabel = (key) => sortLabels[key] || key;

    appRoot.innerHTML = topDepositsTemplate();
    const tableContainer = appRoot.querySelector('#top-deposits-table');
    const summary = appRoot.querySelector('#top-deposits-summary');
    const sortMeta = appRoot.querySelector('#top-deposits-sort');
    const note = appRoot.querySelector('#top-deposits-note');

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
            tableContainer.innerHTML = '<div class="empty">暂无数据</div>';
            return;
        }

        const rows = results.map((item, index) => {
            const rankClass = index === 0 ? 'rank top-1' : index === 1 ? 'rank top-2' : index === 2 ? 'rank top-3' : 'rank';
            return `
                <tr>
                    <td><span class="${rankClass}">${index + 1}</span></td>
                    <td>
                        <div class="address-with-copy address-copy-target" data-address="${item.depositor_address}" title="${item.depositor_address}">
                            <span class="address">${formatAddress(item.depositor_address)}</span>
                        </div>
                    </td>
                    <td>${item.depositor_label || '-'}</td>
                    <td>${formatGweiToAce(item.total_deposit)}</td>
                    <td>${formatNumber(item.validators_total)}</td>
                    <td><span class="badge badge-success">${formatNumber(item.active)}</span></td>
                    <td>${Number(item.slashed) > 0 ? `<span class="badge badge-danger">${formatNumber(item.slashed)}</span>` : '0'}</td>
                    <td>${Number(item.voluntary_exited) > 0 ? `<span class="badge badge-warning">${formatNumber(item.voluntary_exited)}</span>` : '0'}</td>
                    <td>${formatGweiToAce(item.total_active_effective_balance)}</td>
                </tr>
            `;
        }).join('');

        tableContainer.innerHTML = `
            <div class="table-container">
                <table>
                    <colgroup>
                        <col class="col-rank">
                        <col class="col-address">
                        <col class="col-label">
                        <col class="col-total-deposit">
                        <col class="col-validators">
                        <col class="col-active">
                        <col class="col-slashed">
                        <col class="col-voluntary">
                        <col class="col-effective-balance">
                    </colgroup>
                    <thead>
                        <tr>
                            <th>排名</th>
                            <th>存款地址</th>
                            <th>标签</th>
                            <th class="sortable" data-sort="total_deposit">总存款金额 (ACE)</th>
                            <th class="sortable" data-sort="validators_total">Validator 总数</th>
                            <th class="sortable" data-sort="active">活跃数量</th>
                            <th class="sortable" data-sort="slashed">被罚没数量</th>
                            <th class="sortable" data-sort="voluntary_exited">主动退出数量</th>
                            <th class="sortable" data-sort="total_active_effective_balance">总有效余额 (ACE)</th>
                        </tr>
                    </thead>
                    <tbody>${rows}</tbody>
                </table>
            </div>
        `;
        updateSortIndicators();
    };

    const fetchTable = async () => {
        summary.textContent = '列表加载中...';
        sortMeta.textContent = '排序：-';
        note.textContent = '正在获取最新排行';
        tableContainer.innerHTML = '<div class="loading">加载中...</div>';
        let response;
        try {
            response = await fetch(`/deposits/top-deposits?limit=${state.limit}&sort_by=${state.sortBy}&order=${state.order}`, {
                headers: { Accept: 'application/json' },
            });
        } catch (err) {
            if (ticket === renderEpoch) {
                tableContainer.innerHTML = renderError('加载失败，请稍后重试');
                summary.textContent = '加载失败';
                sortMeta.textContent = '排序：-';
                note.textContent = '请求失败，请稍后重试';
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
            tableContainer.innerHTML = renderError((payload && payload.error) || '加载失败');
            summary.textContent = '加载失败';
            sortMeta.textContent = '排序：-';
            note.textContent = '请求失败，请稍后重试';
            return;
        }

        state.limit = payload.limit || state.limit;
        state.sortBy = payload.sort_by || state.sortBy;
        state.order = payload.order === 'asc' ? 'asc' : 'desc';

        const sortLabel = resolveSortLabel(state.sortBy);
        const orderLabel = state.order === 'asc' ? '升序' : '降序';
        summary.textContent = `Top ${state.limit} · ${sortLabel}`;
        sortMeta.textContent = `排序：${sortLabel}（${orderLabel}）`;
        note.textContent = `当前：Top ${state.limit} · ${sortLabel} · ${orderLabel}`;
        renderTable(payload.results || []);
        setQueryParams();
    };

    const handleHeaderClick = (event) => {
        const th = event.target.closest('th.sortable');
        if (!th) return;
        const sortKey = th.getAttribute('data-sort');
        if (!sortKey) return;

        if (sortKey === state.sortBy) {
            state.order = state.order === 'asc' ? 'desc' : 'asc';
        } else {
            state.sortBy = sortKey;
            state.order = 'desc';
        }
        fetchTable();
    };

    cleaner.add(appRoot, 'click', handleHeaderClick);
    await fetchTable();
}

function networkTemplate() {
    return `
        <div id="network-rewards-view" class="network-shell">
            <div class="loading">加载中...</div>
        </div>
        ${footnotesNetwork()}
    `;
}

async function renderNetworkRewards({ ticket }) {
    appRoot.innerHTML = networkTemplate();
    const container = appRoot.querySelector('#network-rewards-view');

    container.innerHTML = '<div class="loading">加载中...</div>';
    let response;
    try {
        response = await fetch('/rewards/network', { headers: { Accept: 'application/json' } });
    } catch (err) {
        if (ticket === renderEpoch) {
            container.innerHTML = renderError('加载失败，请稍后重试');
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
        container.innerHTML = renderError((payload && payload.error) || '暂时无法获取全网收益数据');
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
                    <h2 class="panel-title">历史收益趋势</h2>
                    <p class="panel-note">最近 ${history.length} 个窗口</p>
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
                <h1 class="network-title">全网收益</h1>
                <p class="network-description">
                    汇总最新统计窗口的收益与运行状况，涵盖共识层与执行层的收益。
                </p>
                <div class="network-meta">
                    <div class="meta-item">
                        <span class="meta-label">窗口开始</span>
                        <span class="meta-value">${formatTime(current.window_start)}</span>
                    </div>
                    <div class="meta-item">
                        <span class="meta-label">窗口结束</span>
                        <span class="meta-value">${formatTime(current.window_end)}</span>
                    </div>
                    <div class="meta-item">
                        <span class="meta-label">持续时间</span>
                        <span class="meta-value">${formatDuration(current.window_duration_seconds)}</span>
                    </div>
                </div>
            </div>
            <div class="window-highlight">
                <div class="window-label">活跃 Validator 数量</div>
                <div class="window-value">${formatNumber(current.active_validator_count)}</div>
                <div class="window-subtext">总有效余额：${formatNumber(formatGweiToAce(current.total_effective_balance_gwei))} ACE</div>
                <div class="chip">
                    <span class="chip-dot"></span>
                    <span>窗口截止：${formatTime(current.window_end)}</span>
                </div>
            </div>
        </section>

        <section class="metrics-grid">
            <article class="metric-card accent">
                <div class="metric-label">预估 APR</div>
                <div class="metric-value">${Number(current.project_apr_percent || 0).toFixed(3)}%</div>
                <div class="metric-subtext">基于最后收益窗口</div>
            </article>
            <article class="metric-card">
                <div class="metric-label">总收益 (ACE)</div>
                <div class="metric-value">${formatNumber(formatGweiToAce(current.total_rewards_gwei))}</div>
                <div class="metric-foot">
                    <span class="pill"><span class="dot"></span>CL: ${formatNumber(formatGweiToAce(current.cl_rewards_gwei))}</span>
                    <span class="pill" data-variant="warning"><span class="dot"></span>EL: ${formatNumber(formatGweiToAce(current.el_rewards_gwei))}</span>
                </div>
            </article>
            <article class="metric-card">
                <div class="metric-label">共识层收益</div>
                <div class="metric-value">${formatNumber(formatGweiToAce(current.cl_rewards_gwei))}</div>
                <div class="metric-subtext">单位：ACE</div>
            </article>
            <article class="metric-card">
                <div class="metric-label">执行层收益</div>
                <div class="metric-value">${formatNumber(formatGweiToAce(current.el_rewards_gwei))}</div>
                <div class="metric-subtext">单位：ACE</div>
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
    const labels = sorted.map((h) => new Date(h.window_end).toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' }));
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
                    label: '总收益 (ACE)',
                    data: totalRewards,
                    borderColor: palette.primary,
                    backgroundColor: palette.primaryFill,
                    yAxisID: 'y',
                    tension: 0.2,
                    pointRadius: 3,
                },
                {
                    label: '预估 APR (%)',
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
                                return `总收益: ${formatNumber(context.parsed.y)} ACE`;
                            }
                            return `预估 APR: ${context.parsed.y.toFixed(3)}%`;
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
                    title: { display: true, text: '总收益 (ACE)', color: palette.text },
                    ticks: { color: palette.text },
                    grid: { color: palette.grid, drawBorder: false },
                },
                y1: {
                    type: 'linear',
                    display: true,
                    position: 'right',
                    title: { display: true, text: '预估 APR (%)', color: palette.text },
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
                <h1 class="card-title">地址收益查询</h1>
            </div>

            <form id="query-form">
                <div class="form-group">
                    <label for="address">地址或 Withdrawal Credentials</label>
                    <input type="text" 
                           id="address" 
                           name="address" 
                           placeholder="0x..." 
                           required
                           pattern="0x[a-fA-F0-9]{40}|0x0[12][a-fA-F0-9]{62}">
                    <small style="color: var(--text-secondary); display: block; margin-top: var(--space-1);">
                        支持普通地址（0x + 40 字符）或 withdrawal credentials（0x01/0x02 + 64 字符）
                    </small>
                </div>

                <div class="form-group">
                    <label>
                        <input type="checkbox" name="include_validator_indices" value="true">
                        包含 Validator 索引列表
                    </label>
                </div>

                <button type="submit" id="submit-btn">查询</button>
                <span id="loading" style="margin-left: var(--space-4); display: none;">加载中...</span>
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
                <h2 class="card-title">查询结果</h2>
            </div>

            <div class="stats-grid">
                <div class="stat-card">
                    <div class="stat-label">地址</div>
                    <div class="stat-value address address-copy-target" data-address="${data.address}" title="${data.address}" style="font-size: 1rem;">
                        ${formatAddress(data.address)}
                    </div>
                    ${data.depositor_label ? `<div style="margin-top: var(--space-2); color: var(--text-secondary); font-size: 0.875rem;">${data.depositor_label}</div>` : ''}
                </div>

                <div class="stat-card">
                    <div class="stat-label">活跃 Validator 数量</div>
                    <div class="stat-value">${formatNumber(data.active_validator_count)}</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">CL 收益</div>
                    <div class="stat-value">${formatNumber(formatGweiToAce(data.cl_rewards_gwei))} ACE</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">EL 收益</div>
                    <div class="stat-value">${formatNumber(formatGweiToAce(data.el_rewards_gwei))} ACE</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">总收益</div>
                    <div class="stat-value">${formatNumber(formatGweiToAce(data.total_rewards_gwei))} ACE</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">总有效余额</div>
                    <div class="stat-value">${formatNumber(formatGweiToAce(data.total_effective_balance_gwei))} ACE</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">加权平均质押时间</div>
                    <div class="stat-value">${formatDuration(data.weighted_average_stake_time)}</div>
                </div>

                <div class="stat-card">
                    <div class="stat-label">时间窗口</div>
                    <div class="stat-value" style="font-size: 0.875rem;">
                        ${formatTime(data.window_start)}<br>
                        至<br>
                        ${formatTime(data.window_end)}
                    </div>
                </div>
            </div>

            ${data.validator_indices && data.validator_indices.length > 0 ? `
                <details style="margin-top: var(--space-6);">
                    <summary style="cursor: pointer; font-weight: 600; padding: var(--space-2); background: var(--bg-color); border-radius: var(--radius-md);">
                        Validator 索引列表 (${data.validator_indices.length} 个)
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
            result.innerHTML = renderError('地址不能为空');
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
                result.innerHTML = renderError('请求失败，请稍后再试');
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
            result.innerHTML = renderError((payload && payload.error) || '查询失败');
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
