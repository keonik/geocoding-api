// Admin Dashboard JavaScript
let currentUser = null;
let currentTab = 'users';

// Initialize dashboard
document.addEventListener('DOMContentLoaded', async function() {
    await checkAdminAuth();
    await loadDashboardData();
    await checkSystemStatus();
});

// Authentication
async function checkAdminAuth() {
    const token = localStorage.getItem('authToken');
    if (!token) {
        window.location.href = '/auth/signin?admin=true';
        return;
    }

    try {
        const response = await fetch('/api/v1/user/profile', {
            headers: {
                'Authorization': `Bearer ${token}`
            }
        });

        if (response.ok) {
            const userData = await response.json();
            currentUser = userData.data;
            
            // Check if user is admin
            if (!currentUser.is_admin) {
                alert('Access denied. Admin privileges required.');
                window.location.href = '/';
                return;
            }

            document.getElementById('admin-name').textContent = currentUser.name || currentUser.email;
        } else {
            throw new Error('Authentication failed');
        }
    } catch (error) {
        console.error('Auth error:', error);
        localStorage.removeItem('authToken');
        window.location.href = '/auth/signin?admin=true';
    }
}

function logout() {
    localStorage.removeItem('authToken');
    window.location.href = '/';
}

// Tab Management
function showTab(tabName) {
    // Hide all tab contents
    document.querySelectorAll('.tab-content').forEach(content => {
        content.classList.add('hidden');
    });
    
    // Remove active class from all tabs
    document.querySelectorAll('.tab-button').forEach(button => {
        button.classList.remove('active', 'border-blue-500', 'text-blue-600');
        button.classList.add('border-transparent', 'text-gray-500');
    });
    
    // Show selected tab content
    document.getElementById(`${tabName}-content`).classList.remove('hidden');
    
    // Add active class to selected tab
    const activeTab = document.getElementById(`${tabName}-tab`);
    activeTab.classList.add('active', 'border-blue-500', 'text-blue-600');
    activeTab.classList.remove('border-transparent', 'text-gray-500');
    
    currentTab = tabName;
    
    // Load tab-specific data
    switch(tabName) {
        case 'users':
            loadUsers();
            break;
        case 'api-keys':
            loadAPIKeys();
            break;
        case 'usage':
            loadUsageAnalytics();
            break;
        case 'system':
            checkSystemStatus();
            break;
    }
}

// Dashboard Data Loading
async function loadDashboardData() {
    try {
        // Load overview stats
        await Promise.all([
            loadStats(),
            loadUsers(),
        ]);
    } catch (error) {
        console.error('Error loading dashboard data:', error);
        showNotification('Error loading dashboard data', 'error');
    }
}

async function loadStats() {
    try {
        const token = localStorage.getItem('authToken');
        const response = await fetch('/api/v1/admin/stats', {
            headers: {
                'Authorization': `Bearer ${token}`
            }
        });

        if (response.ok) {
            const data = await response.json();
            const stats = data.data;
            
            document.getElementById('total-users').textContent = stats.total_users || '0';
            document.getElementById('active-keys').textContent = stats.active_keys || '0';
            document.getElementById('calls-today').textContent = stats.calls_today || '0';
            document.getElementById('zip-codes').textContent = stats.zip_codes || '0';
        }
    } catch (error) {
        console.error('Error loading stats:', error);
    }
}

// User Management
async function loadUsers() {
    try {
        const token = localStorage.getItem('authToken');
        const response = await fetch('/api/v1/admin/users', {
            headers: {
                'Authorization': `Bearer ${token}`
            }
        });

        if (response.ok) {
            const data = await response.json();
            displayUsers(data.data);
        }
    } catch (error) {
        console.error('Error loading users:', error);
        showNotification('Error loading users', 'error');
    }
}

function displayUsers(users) {
    const tbody = document.getElementById('users-table');
    tbody.innerHTML = '';

    users.forEach(user => {
        const row = document.createElement('tr');
        row.innerHTML = `
            <td class="px-6 py-4 whitespace-nowrap">
                <div class="flex items-center">
                    <div class="ml-4">
                        <div class="text-sm font-medium text-gray-900">${user.name || 'N/A'}</div>
                        <div class="text-sm text-gray-500">${user.email}</div>
                    </div>
                </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
                <span class="px-2 py-1 text-xs font-semibold rounded-full ${getPlanColor(user.plan_type)}">
                    ${user.plan_type}
                </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
                <span class="px-2 py-1 text-xs font-semibold rounded-full ${user.is_active ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}">
                    ${user.is_active ? 'Active' : 'Inactive'}
                </span>
                ${user.is_admin ? '<span class="ml-1 px-2 py-1 text-xs font-semibold rounded-full bg-purple-100 text-purple-800">Admin</span>' : ''}
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                ${new Date(user.created_at).toLocaleDateString()}
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm font-medium">
                <button onclick="toggleUserStatus(${user.id}, ${user.is_active})" 
                        class="text-indigo-600 hover:text-indigo-900 mr-2">
                    ${user.is_active ? 'Deactivate' : 'Activate'}
                </button>
                <button onclick="makeAdmin(${user.id}, ${user.is_admin})" 
                        class="text-purple-600 hover:text-purple-900 mr-2">
                    ${user.is_admin ? 'Remove Admin' : 'Make Admin'}
                </button>
                <button onclick="viewUserDetails(${user.id})" 
                        class="text-green-600 hover:text-green-900">
                    Details
                </button>
            </td>
        `;
        tbody.appendChild(row);
    });
}

function getPlanColor(plan) {
    const colors = {
        'free': 'bg-gray-100 text-gray-800',
        'basic': 'bg-blue-100 text-blue-800',
        'pro': 'bg-purple-100 text-purple-800',
        'enterprise': 'bg-yellow-100 text-yellow-800'
    };
    return colors[plan] || 'bg-gray-100 text-gray-800';
}

async function toggleUserStatus(userId, currentStatus) {
    if (!confirm(`Are you sure you want to ${currentStatus ? 'deactivate' : 'activate'} this user?`)) {
        return;
    }

    try {
        const token = localStorage.getItem('authToken');
        const response = await fetch(`/api/v1/admin/users/${userId}/status`, {
            method: 'PATCH',
            headers: {
                'Authorization': `Bearer ${token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ is_active: !currentStatus })
        });

        if (response.ok) {
            showNotification('User status updated successfully', 'success');
            loadUsers();
        } else {
            throw new Error('Failed to update user status');
        }
    } catch (error) {
        console.error('Error updating user status:', error);
        showNotification('Error updating user status', 'error');
    }
}

async function makeAdmin(userId, currentAdmin) {
    if (!confirm(`Are you sure you want to ${currentAdmin ? 'remove admin privileges from' : 'make this user an admin'}?`)) {
        return;
    }

    try {
        const token = localStorage.getItem('authToken');
        const response = await fetch(`/api/v1/admin/users/${userId}/admin`, {
            method: 'PATCH',
            headers: {
                'Authorization': `Bearer ${token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ is_admin: !currentAdmin })
        });

        if (response.ok) {
            showNotification('Admin privileges updated successfully', 'success');
            loadUsers();
        } else {
            throw new Error('Failed to update admin privileges');
        }
    } catch (error) {
        console.error('Error updating admin privileges:', error);
        showNotification('Error updating admin privileges', 'error');
    }
}

// API Key Management
async function loadAPIKeys() {
    try {
        const token = localStorage.getItem('authToken');
        const response = await fetch('/api/v1/admin/api-keys', {
            headers: {
                'Authorization': `Bearer ${token}`
            }
        });

        if (response.ok) {
            const data = await response.json();
            displayAPIKeys(data.data);
        }
    } catch (error) {
        console.error('Error loading API keys:', error);
        showNotification('Error loading API keys', 'error');
    }
}

function displayAPIKeys(apiKeys) {
    const tbody = document.getElementById('api-keys-table');
    tbody.innerHTML = '';

    apiKeys.forEach(key => {
        const row = document.createElement('tr');
        row.innerHTML = `
            <td class="px-6 py-4 whitespace-nowrap">
                <code class="text-sm bg-gray-100 px-2 py-1 rounded">${key.key_preview || 'N/A'}</code>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
                <div class="text-sm text-gray-900">${key.user_email}</div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
                <div class="text-sm text-gray-900">${key.name}</div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                ${key.last_used_at ? new Date(key.last_used_at).toLocaleDateString() : 'Never'}
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
                <span class="px-2 py-1 text-xs font-semibold rounded-full ${key.is_active ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}">
                    ${key.is_active ? 'Active' : 'Inactive'}
                </span>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-sm font-medium">
                <button onclick="toggleAPIKeyStatus(${key.id}, ${key.is_active})" 
                        class="text-indigo-600 hover:text-indigo-900 mr-2">
                    ${key.is_active ? 'Disable' : 'Enable'}
                </button>
                <button onclick="deleteAPIKey(${key.id})" 
                        class="text-red-600 hover:text-red-900">
                    Delete
                </button>
            </td>
        `;
        tbody.appendChild(row);
    });
}

// System Management
async function checkSystemStatus() {
    try {
        const token = localStorage.getItem('authToken');
        
        // Check API health
        const healthResponse = await fetch('/api/v1/health');
        document.getElementById('api-status').innerHTML = healthResponse.ok 
            ? '<span class="text-green-600">✓ Healthy</span>' 
            : '<span class="text-red-600">✗ Error</span>';

        // Check database and migrations
        const systemResponse = await fetch('/api/v1/admin/system-status', {
            headers: {
                'Authorization': `Bearer ${token}`
            }
        });

        if (systemResponse.ok) {
            const data = await systemResponse.json();
            const status = data.data;
            
            document.getElementById('db-status').innerHTML = status.database_connected 
                ? '<span class="text-green-600">✓ Connected</span>' 
                : '<span class="text-red-600">✗ Disconnected</span>';
                
            document.getElementById('migrations-status').innerHTML = status.migrations_current 
                ? '<span class="text-green-600">✓ Up to date</span>' 
                : '<span class="text-yellow-600">⚠ Pending</span>';
        }
    } catch (error) {
        console.error('Error checking system status:', error);
    }
}

async function loadZipCodeData() {
    if (!confirm('This will reload all ZIP code data from the CSV file. This may take several minutes. Continue?')) {
        return;
    }

    try {
        const token = localStorage.getItem('authToken');
        showNotification('Loading ZIP code data... This may take a few minutes.', 'info');
        
        const response = await fetch('/api/v1/admin/load-data', {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${token}`
            }
        });

        if (response.ok) {
            showNotification('ZIP code data loaded successfully', 'success');
            loadStats(); // Refresh stats
        } else {
            throw new Error('Failed to load data');
        }
    } catch (error) {
        console.error('Error loading data:', error);
        showNotification('Error loading ZIP code data', 'error');
    }
}

// Utility Functions
function refreshUsers() {
    loadUsers();
}

function refreshAPIKeys() {
    loadAPIKeys();
}

function showNotification(message, type = 'info') {
    // Create notification element
    const notification = document.createElement('div');
    notification.className = `fixed top-4 right-4 z-50 px-4 py-2 rounded-lg shadow-lg text-white ${
        type === 'success' ? 'bg-green-500' : 
        type === 'error' ? 'bg-red-500' : 
        type === 'warning' ? 'bg-yellow-500' : 'bg-blue-500'
    }`;
    notification.textContent = message;
    
    document.body.appendChild(notification);
    
    // Remove after 3 seconds
    setTimeout(() => {
        notification.remove();
    }, 3000);
}

// Usage Analytics (placeholder - would need actual data)
async function loadUsageAnalytics() {
    // This is a placeholder for usage analytics
    // In a real implementation, you'd fetch usage data from your API
    console.log('Loading usage analytics...');
}