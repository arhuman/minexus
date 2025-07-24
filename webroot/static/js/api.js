class MinexusAPI {
    constructor(baseURL = '') {
        this.baseURL = baseURL;
    }

    async getStatus() {
        try {
            const response = await fetch(`${this.baseURL}/api/status`);
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            return await response.json();
        } catch (error) {
            console.error('Failed to fetch status:', error);
            throw error;
        }
    }

    async getMinions() {
        try {
            const response = await fetch(`${this.baseURL}/api/minions`);
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            return await response.json();
        } catch (error) {
            console.error('Failed to fetch minions:', error);
            throw error;
        }
    }

    async getHealth() {
        try {
            const response = await fetch(`${this.baseURL}/api/health`);
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            return await response.json();
        } catch (error) {
            console.error('Failed to fetch health:', error);
            throw error;
        }
    }
}

// Create global API instance
const api = new MinexusAPI();

// Utility functions for formatting
function formatUptime(uptimeSeconds) {
    const days = Math.floor(uptimeSeconds / 86400);
    const hours = Math.floor((uptimeSeconds % 86400) / 3600);
    const minutes = Math.floor((uptimeSeconds % 3600) / 60);
    const seconds = uptimeSeconds % 60;

    let result = '';
    if (days > 0) result += `${days}d `;
    if (hours > 0) result += `${hours}h `;
    if (minutes > 0 || result === '') result += `${minutes}m `;
    if (seconds > 0 && days === 0) result += `${seconds}s`;

    return result.trim();
}

function formatTimestamp(timestamp) {
    return new Date(timestamp).toLocaleString();
}

function getStatusClass(status) {
    switch (status?.toLowerCase()) {
        case 'healthy':
        case 'running':
        case 'active':
            return 'success';
        case 'warning':
            return 'warning';
        case 'error':
        case 'unhealthy':
        case 'stopped':
        case 'inactive':
            return 'error';
        default:
            return 'secondary';
    }
}

// Theme management
function getTheme() {
    return localStorage.getItem('theme') || 'light';
}

function setTheme(theme) {
    localStorage.setItem('theme', theme);
    document.documentElement.setAttribute('data-theme', theme);
}

function toggleTheme() {
    const currentTheme = getTheme();
    const newTheme = currentTheme === 'dark' ? 'light' : 'dark';
    setTheme(newTheme);
}

// Initialize theme on load
document.addEventListener('DOMContentLoaded', function () {
    setTheme(getTheme());
});

// Error handling utilities
function showError(message, container = document.body) {
    const errorDiv = document.createElement('div');
    errorDiv.className = 'error-message';
    errorDiv.style.cssText = `
        position: fixed;
        top: 1rem;
        right: 1rem;
        background: var(--error);
        color: white;
        padding: 1rem;
        border-radius: 6px;
        box-shadow: var(--shadow);
        z-index: 1000;
        max-width: 300px;
    `;
    errorDiv.textContent = message;

    container.appendChild(errorDiv);

    // Auto-remove after 5 seconds
    setTimeout(() => {
        if (errorDiv.parentNode) {
            errorDiv.parentNode.removeChild(errorDiv);
        }
    }, 5000);

    // Click to dismiss
    errorDiv.addEventListener('click', () => {
        if (errorDiv.parentNode) {
            errorDiv.parentNode.removeChild(errorDiv);
        }
    });
}

function showSuccess(message, container = document.body) {
    const successDiv = document.createElement('div');
    successDiv.className = 'success-message';
    successDiv.style.cssText = `
        position: fixed;
        top: 1rem;
        right: 1rem;
        background: var(--success);
        color: white;
        padding: 1rem;
        border-radius: 6px;
        box-shadow: var(--shadow);
        z-index: 1000;
        max-width: 300px;
    `;
    successDiv.textContent = message;

    container.appendChild(successDiv);

    // Auto-remove after 3 seconds
    setTimeout(() => {
        if (successDiv.parentNode) {
            successDiv.parentNode.removeChild(successDiv);
        }
    }, 3000);
}

// Loading indicator utilities
function showLoading(element) {
    if (element) {
        element.classList.add('loading');
        element.innerHTML = '<span class="loading"></span>';
    }
}

function hideLoading(element, originalContent) {
    if (element) {
        element.classList.remove('loading');
        element.innerHTML = originalContent || '';
    }
}