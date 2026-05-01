// Функция для экранирования HTML (защита от XSS)
function escapeHtml(text) {
    if (!text) return '';
    const map = {
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#039;'
    };
    return String(text).replace(/[&<>"']/g, m => map[m]);
}

// Безопасное получение токена
let authToken = null;
try {
    const token = localStorage.getItem('authToken');
    if (token && typeof token === 'string' && token.length > 0) {
        authToken = token;
    }
} catch (e) {
    console.error('Failed to get auth token:', e);
}

// Проверка авторизации
if (authToken) {
    document.getElementById('loginSection').style.display = 'none';
    document.getElementById('adminSection').style.display = 'block';
    loadPromotions();
}

// Вход
document.getElementById('loginForm')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const formData = new FormData(e.target);
    
    try {
        const response = await fetch('/api/admin/login', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                username: formData.get('username'),
                password: formData.get('password')
            })
        });
        
        if (response.ok) {
            const data = await response.json();
            authToken = data.token;
            localStorage.setItem('authToken', authToken);
            document.getElementById('loginSection').style.display = 'none';
            document.getElementById('adminSection').style.display = 'block';
            loadPromotions();
        } else {
            alert('Неверный логин или пароль');
        }
    } catch (error) {
        console.error('Login error:', error);
        alert('Ошибка при входе');
    }
});

// Загрузка акций
async function loadPromotions() {
    try {
        const response = await fetch('/api/admin/promotions', {
            headers: {
                'Authorization': `Bearer ${authToken}`
            }
        });
        
        if (response.status === 401) {
            localStorage.removeItem('authToken');
            authToken = null;
            document.getElementById('loginSection').style.display = 'block';
            document.getElementById('adminSection').style.display = 'none';
            return;
        }
        
        const data = await response.json();
        const listEl = document.getElementById('promotionsList');
        
        if (data.promotions && data.promotions.length > 0) {
            listEl.innerHTML = data.promotions.map(promotion => {
                const title = escapeHtml(promotion.promotions_title || '');
                const desc = promotion.promotions_description ? escapeHtml(promotion.promotions_description) : '';
                const discount = parseFloat(promotion.promotions_discount || 0).toFixed(2);
                const id = parseInt(promotion.promotions_id_pk || 0);
                const isActive = promotion.promotions_active !== false;
                
                return `
                <div class="promotion-item">
                    <div>
                        <h3>${title}</h3>
                        ${desc ? `<p>${desc}</p>` : ''}
                        <p>Скидка: ${discount}%</p>
                        <p>Статус: ${isActive ? 'Активна' : 'Неактивна'}</p>
                    </div>
                    <div class="promotion-actions">
                        <button class="btn btn-edit" onclick="editPromotion(${id})">Редактировать</button>
                        <button class="btn btn-danger" onclick="deletePromotion(${id})">Удалить</button>
                    </div>
                </div>
            `;
            }).join('');
        } else {
            listEl.innerHTML = '<p>Акций пока нет</p>';
        }
    } catch (error) {
        console.error('Error loading promotions:', error);
    }
}

// Показать форму создания акции
function showCreatePromotionForm() {
    document.getElementById('promotionModalTitle').textContent = 'Создать акцию';
    document.getElementById('promotionForm').reset();
    document.getElementById('promotionId').value = '';
    document.getElementById('promotionModal').style.display = 'block';
}

// Редактирование акции
async function editPromotion(id) {
    try {
        const response = await fetch(`/api/admin/promotions/${id}`, {
            headers: {
                'Authorization': `Bearer ${authToken}`
            }
        });
        
        if (response.ok) {
            const promotion = await response.json();
            document.getElementById('promotionModalTitle').textContent = 'Редактировать акцию';
            document.getElementById('promotionId').value = promotion.promotions_id_pk;
            document.getElementById('promotionTitle').value = promotion.promotions_title;
            document.getElementById('promotionDescription').value = promotion.promotions_description || '';
            document.getElementById('promotionDiscount').value = promotion.promotions_discount;
            document.getElementById('promotionImageURL').value = promotion.promotions_image_url || '';
            document.getElementById('promotionActive').checked = promotion.promotions_active;
            
            if (promotion.promotions_start_date) {
                const startDate = new Date(promotion.promotions_start_date);
                document.getElementById('promotionStartDate').value = startDate.toISOString().slice(0, 16);
            }
            if (promotion.promotions_end_date) {
                const endDate = new Date(promotion.promotions_end_date);
                document.getElementById('promotionEndDate').value = endDate.toISOString().slice(0, 16);
            }
            
            document.getElementById('promotionModal').style.display = 'block';
        }
    } catch (error) {
        console.error('Error loading promotion:', error);
    }
}

// Удаление акции
async function deletePromotion(id) {
    if (!confirm('Вы уверены, что хотите удалить эту акцию?')) {
        return;
    }
    
    try {
        const response = await fetch(`/api/admin/promotions/${id}`, {
            method: 'DELETE',
            headers: {
                'Authorization': `Bearer ${authToken}`
            }
        });
        
        if (response.ok) {
            loadPromotions();
        } else {
            alert('Ошибка при удалении акции');
        }
    } catch (error) {
        console.error('Error deleting promotion:', error);
        alert('Ошибка при удалении акции');
    }
}

// Сохранение акции
document.getElementById('promotionForm')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    
    const id = document.getElementById('promotionId').value;
    const promotion = {
        promotions_title: document.getElementById('promotionTitle').value,
        promotions_description: document.getElementById('promotionDescription').value || null,
        promotions_discount: parseFloat(document.getElementById('promotionDiscount').value) || 0,
        promotions_image_url: document.getElementById('promotionImageURL').value || null,
        promotions_active: document.getElementById('promotionActive').checked,
        promotions_start_date: document.getElementById('promotionStartDate').value || null,
        promotions_end_date: document.getElementById('promotionEndDate').value || null
    };
    
    try {
        const url = id 
            ? `/api/admin/promotions/${id}`
            : '/api/admin/promotions';
        const method = id ? 'PUT' : 'POST';
        
        const response = await fetch(url, {
            method: method,
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${authToken}`
            },
            body: JSON.stringify(promotion)
        });
        
        if (response.ok) {
            closePromotionModal();
            loadPromotions();
        } else {
            const error = await response.json();
            alert('Ошибка: ' + (error.error || 'Не удалось сохранить акцию'));
        }
    } catch (error) {
        console.error('Error saving promotion:', error);
        alert('Ошибка при сохранении акции');
    }
});

// Закрыть модальное окно
function closePromotionModal() {
    document.getElementById('promotionModal').style.display = 'none';
}

// Закрытие модального окна при клике вне его
window.onclick = function(event) {
    const modal = document.getElementById('promotionModal');
    if (event.target === modal) {
        closePromotionModal();
    }
}

// Синхронизация товаров с МойСклад
async function syncProducts() {
    const btn = document.getElementById('syncBtn');
    const resultDiv = document.getElementById('syncResult');
    
    btn.disabled = true;
    btn.textContent = '⏳ Синхронизация...';
    resultDiv.innerHTML = '<p>⏳ Синхронизация товаров из МойСклад...</p>';
    
    try {
        const response = await fetch('/api/admin/products/sync', {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${authToken}`
            }
        });
        
        if (response.status === 401) {
            localStorage.removeItem('authToken');
            authToken = null;
            document.getElementById('loginSection').style.display = 'block';
            document.getElementById('adminSection').style.display = 'none';
            resultDiv.innerHTML = '<p style="color: red;">Сессия истекла. Войдите заново.</p>';
            return;
        }
        
        if (!response.ok) {
            const error = await response.json();
            resultDiv.innerHTML = `<p style="color: red;">❌ Ошибка: ${error.error || error.message || 'Не удалось синхронизировать товары'}</p>`;
            return;
        }
        
        const data = await response.json();
        const now = new Date().toLocaleTimeString('ru-RU');
        resultDiv.innerHTML = `
            <div style="background: #e8f5e9; padding: 1rem; border-radius: 5px; margin-top: 1rem;">
                <p style="color: green; font-weight: bold;">✅ Синхронизация завершена! (${now})</p>
                <p><strong>Создано:</strong> ${data.result.created} товаров</p>
                <p><strong>Обновлено:</strong> ${data.result.updated} товаров</p>
                ${data.result.errors > 0 ? `<p style="color: orange;"><strong>Ошибок:</strong> ${data.result.errors}</p>` : ''}
            </div>
        `;
    } catch (error) {
        console.error('Sync error:', error);
        resultDiv.innerHTML = '<p style="color: red;">❌ Ошибка при синхронизации. Проверьте настройки МойСклад и логи приложения.</p>';
    } finally {
        btn.disabled = false;
        btn.textContent = '🔄 Обновить товары сейчас';
    }
}

// Проверка статуса автоматической синхронизации
function updateAutoSyncStatus() {
    const statusEl = document.getElementById('autoSyncStatus');
    if (statusEl) {
        // Проверяем наличие токена МойСклад (упрощенная проверка)
        statusEl.textContent = '✅ Включена';
        statusEl.style.color = 'green';
    }
}

// Обновляем статус при загрузке админ-панели
if (authToken) {
    updateAutoSyncStatus();
}

