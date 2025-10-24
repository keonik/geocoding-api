// Dashboard functionality
let currentUser = null;
let apiKeys = [];

// Initialize dashboard
document.addEventListener('DOMContentLoaded', function() {
    // Check if user is logged in
    const userData = localStorage.getItem('user');
    if (!userData) {
        window.location.href = '/auth/signin';
        return;
    }
    
    currentUser = JSON.parse(userData);
    initializeDashboard();
});

function initializeDashboard() {
    // Update user info
    document.getElementById('user-email').textContent = currentUser.email;
    document.getElementById('current-plan').textContent = currentUser.plan_type;
    
    // Load API keys and usage
    loadAPIKeys();
    
    // Set up event listeners
    setupEventListeners();
}

function setupEventListeners() {
    // Logout
    document.getElementById('logout-btn').addEventListener('click', function() {
        localStorage.removeItem('user');
        window.location.href = '/';
    });
    
    // Create API Key modal
    document.getElementById('create-key-btn').addEventListener('click', function() {
        document.getElementById('create-key-modal').classList.remove('hidden');
    });
    
    document.getElementById('cancel-key-btn').addEventListener('click', function() {
        document.getElementById('create-key-modal').classList.add('hidden');
    });
    
    // Create API Key form
    document.getElementById('create-key-form').addEventListener('submit', createAPIKey);
    
    // Copy API key
    document.getElementById('copy-key-btn').addEventListener('click', function() {
        const keyInput = document.getElementById('new-api-key');
        keyInput.select();
        document.execCommand('copy');
        
        // Show feedback
        const btn = document.getElementById('copy-key-btn');
        const originalText = btn.innerHTML;
        btn.innerHTML = '<i class="fas fa-check"></i>';
        btn.classList.remove('bg-blue-600', 'hover:bg-blue-700');
        btn.classList.add('bg-green-600');
        
        setTimeout(() => {
            btn.innerHTML = originalText;
            btn.classList.remove('bg-green-600');
            btn.classList.add('bg-blue-600', 'hover:bg-blue-700');
        }, 2000);
    });
    
    // Close show key modal
    document.getElementById('close-key-modal-btn').addEventListener('click', function() {
        document.getElementById('show-key-modal').classList.add('hidden');
        loadAPIKeys(); // Refresh the list
    });
    
    // Handle all permissions checkbox
    const checkboxes = document.querySelectorAll('.permission-checkbox');
    const allPermissionsCheckbox = checkboxes[0]; // First checkbox is "all permissions"
    
    allPermissionsCheckbox.addEventListener('change', function() {
        if (this.checked) {
            checkboxes.forEach((cb, index) => {
                if (index > 0) cb.checked = false;
            });
        }
    });
    
    // Handle individual permission checkboxes
    for (let i = 1; i < checkboxes.length; i++) {
        checkboxes[i].addEventListener('change', function() {
            if (this.checked) {
                allPermissionsCheckbox.checked = false;
            }
        });
    }
}

async function loadAPIKeys() {
    try {
        // Load usage stats
        const usageResponse = await fetch('/api/v1/user/usage', {
            headers: {
                'X-User-ID': currentUser.id.toString()
            }
        });
        
        if (usageResponse.ok) {
            const usageData = await usageResponse.json();
            updateUsageStats(usageData.data);
        }
        
        // Load API keys
        const keysResponse = await fetch('/api/v1/user/api-keys', {
            headers: {
                'X-User-ID': currentUser.id.toString()
            }
        });
        
        if (keysResponse.ok) {
            const keysData = await keysResponse.json();
            if (keysData.success) {
                renderAPIKeys(keysData.data.api_keys || []);
                document.getElementById('api-key-count').textContent = keysData.data.count || 0;
            } else {
                console.error('Failed to load API keys:', keysData.error);
                renderAPIKeys([]);
            }
        } else {
            console.error('Failed to fetch API keys:', keysResponse.status);
            renderAPIKeys([]);
        }
        
    } catch (error) {
        console.error('Error loading dashboard data:', error);
        document.getElementById('loading-keys').innerHTML = `
            <div class="p-8 text-center text-red-500">
                <i class="fas fa-exclamation-triangle text-2xl mb-2"></i>
                <p>Error loading data. Please refresh the page.</p>
            </div>
        `;
        renderAPIKeys([]);
    }
}

function updateUsageStats(data) {
    document.getElementById('current-usage').textContent = data.current_usage || 0;
    document.getElementById('monthly-limit').textContent = data.monthly_limit || 1000;
    // API key count will be updated separately when we load actual keys
}

function renderAPIKeys(keys) {
    const container = document.getElementById('api-keys-container');
    
    if (!keys || keys.length === 0) {
        container.innerHTML = `
            <div class="p-8 text-center text-gray-500">
                <i class="fas fa-key text-4xl mb-4 text-gray-300"></i>
                <h3 class="text-lg font-medium mb-2">No API Keys Yet</h3>
                <p class="mb-4">Create your first API key to start using the GeoCode API.</p>
                <button class="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-md" onclick="document.getElementById('create-key-btn').click()">
                    <i class="fas fa-plus mr-2"></i>Create Your First Key
                </button>
            </div>
        `;
        return;
    }
    
    // Render actual API keys
    const keysList = keys.map(key => `
        <div class="border-b border-gray-200 p-4 hover:bg-gray-50 last:border-b-0">
            <div class="flex justify-between items-start">
                <div class="flex-1">
                    <h4 class="font-medium text-gray-900">${escapeHtml(key.name)}</h4>
                    <p class="text-sm text-gray-500 font-mono">${escapeHtml(key.key_preview)}</p>
                    <div class="mt-2 flex flex-wrap gap-1">
                        ${key.permissions.map(perm => `
                            <span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-blue-100 text-blue-800">
                                ${perm === '*' ? 'All permissions' : escapeHtml(perm)}
                            </span>
                        `).join('')}
                    </div>
                    <div class="mt-1 flex items-center space-x-4 text-xs text-gray-500">
                        <span>Created ${new Date(key.created_at).toLocaleDateString()}</span>
                        ${key.last_used_at ? `<span>Last used ${new Date(key.last_used_at).toLocaleDateString()}</span>` : '<span>Never used</span>'}
                    </div>
                </div>
                <div class="flex space-x-2">
                    <button class="text-red-600 hover:text-red-800 text-sm" onclick="deleteAPIKey('${key.id}')" title="Delete API key">
                        <i class="fas fa-trash"></i>
                    </button>
                </div>
            </div>
        </div>
    `).join('');
    
    container.innerHTML = `<div class="divide-y divide-gray-200">${keysList}</div>`;
}

// Helper function to escape HTML
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

async function createAPIKey(e) {
    e.preventDefault();
    
    const name = document.getElementById('key-name').value;
    const selectedPermissions = Array.from(document.querySelectorAll('.permission-checkbox:checked'))
        .map(cb => cb.value);
    
    if (selectedPermissions.length === 0) {
        alert('Please select at least one permission');
        return;
    }
    
    try {
        const response = await fetch('/api/v1/user/api-keys', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-User-ID': currentUser.id.toString()
            },
            body: JSON.stringify({
                name: name,
                permissions: selectedPermissions
            })
        });
        
        const data = await response.json();
        
        if (data.success) {
            // Hide create modal
            document.getElementById('create-key-modal').classList.add('hidden');
            
            // Show the new API key
            document.getElementById('new-api-key').value = data.data.key_string;
            document.getElementById('show-key-modal').classList.remove('hidden');
            
            // Reset form
            document.getElementById('create-key-form').reset();
        } else {
            alert('Error creating API key: ' + data.error);
        }
    } catch (error) {
        alert('Network error. Please try again.');
    }
}

async function deleteAPIKey(keyId) {
    if (!confirm('Are you sure you want to delete this API key? This action cannot be undone.')) {
        return;
    }
    
    try {
        const response = await fetch(`/api/v1/user/api-keys/${keyId}`, {
            method: 'DELETE',
            headers: {
                'X-User-ID': currentUser.id.toString()
            }
        });
        
        const data = await response.json();
        
        if (data.success) {
            // Show success message
            alert('API key deleted successfully');
            // Refresh the API keys list
            loadAPIKeys();
        } else {
            alert('Error deleting API key: ' + data.error);
        }
    } catch (error) {
        console.error('Error deleting API key:', error);
        alert('Network error. Please try again.');
    }
}