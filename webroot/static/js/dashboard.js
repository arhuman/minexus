document.addEventListener('DOMContentLoaded', function () {
    let refreshInterval;
    let isAutoRefreshEnabled = true;

    // Initialize dashboard
    initializeDashboard();
    setupEventListeners();
    startAutoRefresh();

    function initializeDashboard() {
        // Add theme toggle button if not exists
        addThemeToggle();

        // Add auto-refresh indicator
        addAutoRefreshIndicator();

        // Initial data load
        refreshDashboard();
    }

    function setupEventListeners() {
        // Theme toggle
        const themeToggle = document.getElementById('theme-toggle');
        if (themeToggle) {
            themeToggle.addEventListener('click', toggleTheme);
        }

        // Auto-refresh toggle
        const autoRefreshToggle = document.getElementById('auto-refresh-toggle');
        if (autoRefreshToggle) {
            autoRefreshToggle.addEventListener('click', toggleAutoRefresh);
        }

        // Manual refresh button
        const refreshButton = document.getElementById('refresh-button');
        if (refreshButton) {
            refreshButton.addEventListener('click', () => {
                refreshDashboard();
                showSuccess('Dashboard refreshed');
            });
        }

        // Handle visibility change (pause refresh when tab not visible)
        document.addEventListener('visibilitychange', function () {
            if (document.hidden) {
                stopAutoRefresh();
            } else if (isAutoRefreshEnabled) {
                startAutoRefresh();
            }
        });
    }

    function addThemeToggle() {
        if (document.getElementById('theme-toggle')) return;

        const themeToggle = document.createElement('button');
        themeToggle.id = 'theme-toggle';
        themeToggle.className = 'theme-toggle';
        themeToggle.innerHTML = '🌙';
        themeToggle.title = 'Toggle dark mode';
        document.body.appendChild(themeToggle);

        // Update icon based on current theme
        updateThemeToggleIcon();
    }

    function updateThemeToggleIcon() {
        const themeToggle = document.getElementById('theme-toggle');
        if (themeToggle) {
            const isDark = getTheme() === 'dark';
            themeToggle.innerHTML = isDark ? '☀️' : '🌙';
            themeToggle.title = isDark ? 'Switch to light mode' : 'Switch to dark mode';
        }
    }

    function addAutoRefreshIndicator() {
        if (document.getElementById('auto-refresh')) return;

        const indicator = document.createElement('div');
        indicator.id = 'auto-refresh';
        indicator.className = 'auto-refresh';
        indicator.innerHTML = `
            <span id="auto-refresh-toggle" style="cursor: pointer;" title="Toggle auto-refresh">
                🔄 Auto-refresh: <span id="refresh-status">ON</span>
            </span>
            <span id="refresh-countdown" style="margin-left: 0.5rem;"></span>
        `;
        document.body.appendChild(indicator);
    }

    function startAutoRefresh() {
        stopAutoRefresh(); // Clear any existing interval

        let countdown = 30; // 30 seconds
        updateRefreshCountdown(countdown);

        refreshInterval = setInterval(() => {
            countdown--;
            updateRefreshCountdown(countdown);

            if (countdown <= 0) {
                refreshDashboard();
                countdown = 30; // Reset countdown
            }
        }, 1000);

        updateAutoRefreshStatus(true);
    }

    function stopAutoRefresh() {
        if (refreshInterval) {
            clearInterval(refreshInterval);
            refreshInterval = null;
        }
        updateRefreshCountdown(0);
    }

    function toggleAutoRefresh() {
        isAutoRefreshEnabled = !isAutoRefreshEnabled;

        if (isAutoRefreshEnabled) {
            startAutoRefresh();
        } else {
            stopAutoRefresh();
        }

        updateAutoRefreshStatus(isAutoRefreshEnabled);
    }

    function updateAutoRefreshStatus(enabled) {
        const status = document.getElementById('refresh-status');
        const indicator = document.getElementById('auto-refresh');

        if (status) {
            status.textContent = enabled ? 'ON' : 'OFF';
        }

        if (indicator) {
            indicator.className = enabled ? 'auto-refresh active' : 'auto-refresh';
        }
    }

    function updateRefreshCountdown(seconds) {
        const countdown = document.getElementById('refresh-countdown');
        if (countdown) {
            if (seconds > 0) {
                countdown.textContent = `(${seconds}s)`;
            } else {
                countdown.textContent = '';
            }
        }
    }

    async function refreshDashboard() {
        try {
            // Show loading state
            showLoadingState();

            // Fetch all data concurrently
            const [statusData, minionsData] = await Promise.all([
                api.getStatus(),
                api.getMinions()
            ]);

            // Update dashboard sections
            updateSystemStatus(statusData);
            updateMinionStatus(minionsData);
            updateServerPorts(statusData);

            // Hide loading state
            hideLoadingState();

        } catch (error) {
            console.error('Failed to refresh dashboard:', error);
            showError('Failed to refresh dashboard data');
            hideLoadingState();
        }
    }

    function showLoadingState() {
        const statusCards = document.querySelectorAll('.status-card');
        statusCards.forEach(card => {
            const indicator = card.querySelector('.status-indicator');
            if (indicator) {
                indicator.classList.add('loading');
            }
        });
    }

    function hideLoadingState() {
        const statusCards = document.querySelectorAll('.status-card');
        statusCards.forEach(card => {
            const indicator = card.querySelector('.status-indicator');
            if (indicator) {
                indicator.classList.remove('loading');
            }
        });
    }

    function updateSystemStatus(data) {
        const versionElement = document.querySelector('.status-card p:nth-of-type(1)');
        const uptimeElement = document.querySelector('.status-card p:nth-of-type(2)');
        const statusIndicator = document.querySelector('.status-indicator');

        if (versionElement) {
            versionElement.textContent = `Version: ${data.version || 'Unknown'}`;
        }

        if (uptimeElement) {
            uptimeElement.textContent = `Uptime: ${data.uptime || 'Unknown'}`;
        }

        if (statusIndicator) {
            const isHealthy = data.servers &&
                data.servers.minion?.status === 'running' &&
                data.servers.console?.status === 'running';
            const status = isHealthy ? 'healthy' : 'warning';

            statusIndicator.className = `status-indicator ${status}`;
            statusIndicator.textContent = status.toUpperCase();
        }
    }

    function updateMinionStatus(data) {
        const countElement = document.querySelector('.minion-count');
        const listElement = document.querySelector('.minion-list');

        if (countElement) {
            countElement.textContent = data.count || 0;
        }

        if (listElement) {
            listElement.innerHTML = '';

            if (data.minions && data.minions.length > 0) {
                data.minions.forEach(minion => {
                    const minionItem = document.createElement('div');
                    minionItem.className = 'minion-item';
                    minionItem.innerHTML = `
                        <span class="minion-id">${minion.id}</span>
                        <span class="minion-status ${minion.status}">${minion.status}</span>
                    `;
                    listElement.appendChild(minionItem);
                });
            } else {
                const noMinions = document.createElement('div');
                noMinions.className = 'minion-item';
                noMinions.innerHTML = '<span class="minion-id">No minions connected</span>';
                listElement.appendChild(noMinions);
            }
        }
    }

    function updateServerPorts(data) {
        const portList = document.querySelector('.port-list');
        if (!portList || !data.servers) return;

        const ports = [
            { name: 'Minion (gRPC)', port: data.servers.minion?.port, status: data.servers.minion?.status },
            { name: 'Console (mTLS)', port: data.servers.console?.port, status: data.servers.console?.status },
            { name: 'Web (HTTP)', port: data.servers.web?.port, status: data.servers.web?.status }
        ];

        portList.innerHTML = '';

        ports.forEach(portInfo => {
            const portItem = document.createElement('div');
            portItem.className = 'port-item';
            portItem.innerHTML = `
                <span>${portInfo.name}: ${portInfo.port || 'Unknown'}</span>
                <span class="status-dot ${portInfo.status === 'running' ? 'running' : 'stopped'}"></span>
            `;
            portList.appendChild(portItem);
        });
    }

    // Override theme toggle to update icon
    const originalToggleTheme = window.toggleTheme;
    window.toggleTheme = function () {
        if (originalToggleTheme) {
            originalToggleTheme();
        }
        updateThemeToggleIcon();
    };

    // Add keyboard shortcuts
    document.addEventListener('keydown', function (e) {
        // Ctrl/Cmd + R for refresh
        if ((e.ctrlKey || e.metaKey) && e.key === 'r') {
            e.preventDefault();
            refreshDashboard();
            showSuccess('Dashboard refreshed');
        }

        // Ctrl/Cmd + D for dark mode toggle
        if ((e.ctrlKey || e.metaKey) && e.key === 'd') {
            e.preventDefault();
            toggleTheme();
        }

        // Space to toggle auto-refresh
        if (e.code === 'Space' && e.target === document.body) {
            e.preventDefault();
            toggleAutoRefresh();
        }
    });

    // Handle connection errors gracefully
    window.addEventListener('online', function () {
        showSuccess('Connection restored');
        if (isAutoRefreshEnabled) {
            startAutoRefresh();
        }
    });

    window.addEventListener('offline', function () {
        showError('Connection lost');
        stopAutoRefresh();
    });
});