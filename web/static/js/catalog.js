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

// Безопасный парсинг JSON из localStorage
function safeParseJSON(str, defaultValue) {
    try {
        return JSON.parse(str) || defaultValue;
    } catch (e) {
        return defaultValue;
    }
}

let cart = safeParseJSON(localStorage.getItem('cart'), []);

// Пагинация
const pageLimit = 25; // Кратно 5 колонкам
let currentOffset = 0;
let currentQuery = '';
let currentCategoryId = '';
let totalPages = 1;
let currentPage = 1;
let totalProducts = 0;

// Генерация массива страниц для пагинации
function generatePaginationArray(current, total, maxVisible = 7) {
    if (total <= maxVisible) {
        return Array.from({ length: total }, (_, i) => i + 1);
    }
    
    const pages = [];
    const halfVisible = Math.floor(maxVisible / 2);
    
    // Всегда показываем первую страницу
    pages.push(1);
    
    if (current > halfVisible + 2) {
        pages.push('...');
    }
    
    // Показываем страницы вокруг текущей
    const start = Math.max(2, current - halfVisible);
    const end = Math.min(total - 1, current + halfVisible);
    
    for (let i = start; i <= end; i++) {
        if (i > 1 && i < total) {
            pages.push(i);
        }
    }
    
    if (current < total - halfVisible - 1) {
        pages.push('...');
    }
    
    // Всегда показываем последнюю страницу
    if (total > 1) {
        pages.push(total);
    }
    
    return pages;
}

function setPaginationUI(hasNext, totalCount = 0) {
    const paginationContainer = document.getElementById('paginationContainer');
    const prevBtn = document.getElementById('prevPageBtn');
    const nextBtn = document.getElementById('nextPageBtn');
    const firstBtn = document.getElementById('firstPageBtn');
    const lastBtn = document.getElementById('lastPageBtn');
    
    // Вычисляем общее количество страниц
    if (totalCount > 0) {
        totalPages = Math.ceil(totalCount / pageLimit);
    } else if (hasNext) {
        // Если есть следующая страница, но нет общего количества
        totalPages = currentPage + 2; // Минимум текущая + следующая
    } else {
        totalPages = currentPage;
    }
    
    currentPage = Math.floor(currentOffset / pageLimit) + 1;
    
    // Генерируем HTML для пагинации
    const pages = generatePaginationArray(currentPage, totalPages);
    
    let paginationHTML = `
        <div class="pagination-wrapper">
            <div class="pagination-controls">
                <button class="btn btn-secondary" id="firstPageBtn" onclick="goToPage(1)" 
                        ${currentPage === 1 ? 'disabled' : ''} title="В начало">
                    ««
                </button>
                <button class="btn btn-secondary" id="prevPageBtn" onclick="prevPage()" 
                        ${currentPage === 1 ? 'disabled' : ''} title="Назад">
                    «
                </button>
    `;
    
    // Добавляем номера страниц
    pages.forEach(page => {
        if (page === '...') {
            paginationHTML += '<span class="pagination-ellipsis">...</span>';
        } else {
            const isActive = page === currentPage;
            paginationHTML += `
                <button class="btn ${isActive ? 'btn-primary' : 'btn-outline'}" 
                        onclick="goToPage(${page})" 
                        ${isActive ? 'disabled' : ''}>
                    ${page}
                </button>
            `;
        }
    });
    
    paginationHTML += `
                <button class="btn btn-secondary" id="nextPageBtn" onclick="nextPage()" 
                        ${!hasNext ? 'disabled' : ''} title="Далее">
                    »
                </button>
                <button class="btn btn-secondary" id="lastPageBtn" onclick="goToPage(${totalPages})" 
                        ${currentPage === totalPages ? 'disabled' : ''} title="В конец">
                    »»
                </button>
            </div>
            <div class="pagination-info">
                <span>Страница ${currentPage} ${totalPages > 1 ? `из ${totalPages}` : ''}</span>
                ${totalCount > 0 ? `<span class="total-products">Всего товаров: ${totalCount}</span>` : ''}
            </div>
        </div>
    `;
    
    if (paginationContainer) {
        paginationContainer.innerHTML = paginationHTML;
    }
    
    // Плавная прокрутка к началу каталога
    scrollToTop();
}

// Загрузка товаров
async function loadProducts(query = '', append = false) {
    try {
        showLoading(!append);
        
        let url;
        if (query) {
            url = `/api/products/search?q=${encodeURIComponent(query)}&limit=${pageLimit}&offset=${currentOffset}`;
        } else {
            url = `/api/products?limit=${pageLimit}&offset=${currentOffset}`;
            if (currentCategoryId) {
                url += `&category_id=${encodeURIComponent(currentCategoryId)}`;
            }
        }
        const response = await fetch(url);
        const data = await response.json();
        const grid = document.getElementById('productsGrid');
        
        if (data.products && data.products.length > 0) {
            const productsHTML = data.products.map(product => {
                // Поддержка обоих форматов: старый (products_*) и новый (ID, Name, Price)
                const productId = parseInt(product.ID || product.products_id_pk || 0);
                const inCart = cart.find(item => item.id === productId);
                const name = escapeHtml(product.Name || product.products_name || '');
                const image = (product.ImageURL || product.products_image_url) ? escapeHtml(product.ImageURL || product.products_image_url) : '';
                const price = parseFloat(product.Price || product.products_price || 0).toFixed(2);
                const stock = parseInt(product.Stock || product.products_stock || 0);
                const isActive = (product.Active !== undefined ? product.Active : product.products_active) !== false;
                
                // Определяем классы для остатков
                let stockClass = '';
                let stockText = `В наличии: ${stock} шт.`;
                
                if (stock > 0 && stock < 5) {
                    stockClass = 'stock-low';
                    stockText = `Осталось мало: ${stock} шт.`;
                } else if (stock === 0) {
                    stockClass = 'stock-out';
                    stockText = 'Нет в наличии';
                }
                
                // Безопасная передача данных в функцию
                const nameEscaped = name.replace(/'/g, "\\'");
                
                // Плейсхолдер для изображения, если нет
                const imagePlaceholder = image || 'data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjUwIiBoZWlnaHQ9IjI1MCIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj48cmVjdCB3aWR0aD0iMjUwIiBoZWlnaHQ9IjI1MCIgZmlsbD0iI2YwZjBmMCIvPjx0ZXh0IHg9IjUwJSIgeT0iNTAlIiBmb250LWZhbWlseT0iQXJpYWwiIGZvbnQtc2l6ZT0iMTgiIGZpbGw9IiM5OTkiIHRleHQtYW5jaG9yPSJtaWRkbGUiIGR5PSIuM2VtIj5ObyBJbWFnZTwvdGV4dD48L3N2Zz4=';
                
                return `
                    <a href="/product/${productId}" class="product-card">
                        <img src="${imagePlaceholder}" alt="${name}" onerror="this.src='data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjUwIiBoZWlnaHQ9IjI1MCIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj48cmVjdCB3aWR0aD0iMjUwIiBoZWlnaHQ9IjI1MCIgZmlsbD0iI2YwZjBmMCIvPjx0ZXh0IHg9IjUwJSIgeT0iNTAlIiBmb250LWZhbWlseT0iQXJpYWwiIGZvbnQtc2l6ZT0iMTgiIGZpbGw9IiM5OTkiIHRleHQtYW5jaG9yPSJtaWRkbGUiIGR5PSIuM2VtIj5ObyBJbWFnZTwvdGV4dD48L3N2Zz4='">
                        <div class="product-card-content">
                            <h3>${name}</h3>
                            <p class="price">${price} руб.</p>
                            <p class="stock ${stockClass}">${stockText}</p>
                            <button class="btn" onclick="event.preventDefault(); addToCart(${productId}, '${nameEscaped}', ${price}); return false;" 
                                    ${!isActive || stock === 0 ? 'disabled' : ''}>
                                ${inCart ? 'В корзине' : 'В корзину'}
                            </button>
                        </div>
                    </a>
                `;
            }).join('');
            
            if (append) {
                grid.innerHTML += productsHTML;
            } else {
                grid.innerHTML = productsHTML;
            }
            
            // Обновляем пагинацию
            const totalCount = data.total || data.count || 0;
            setPaginationUI(data.products.length >= pageLimit, totalCount);
        } else {
            if (!append) {
                grid.innerHTML = '<div class="no-products"><p>Товары не найдены</p></div>';
            }
            setPaginationUI(false, 0);
        }
    } catch (error) {
        console.error('Error loading products:', error);
        if (!append) {
            document.getElementById('productsGrid').innerHTML = '<div class="error-message"><p>Ошибка загрузки товаров</p></div>';
        }
        setPaginationUI(false, 0);
    } finally {
        showLoading(false);
    }
}

// Поиск товаров
function searchProducts() {
    const query = document.getElementById('searchInput').value;
    currentQuery = (query || '').trim();
    currentOffset = 0;
    currentCategoryId = '';
    document.getElementById('subcategoryBar').hidden = true;
    renderCategoryBar();
    updateBreadcrumb();
    loadProducts(currentQuery);
}

function nextPage() {
    currentOffset += pageLimit;
    loadProducts(currentQuery);
}

function prevPage() {
    currentOffset = Math.max(0, currentOffset - pageLimit);
    loadProducts(currentQuery);
}

function goToPage(page) {
    if (page < 1 || page > totalPages) return;
    currentOffset = (page - 1) * pageLimit;
    loadProducts(currentQuery);
}

function goToFirstPage() {
    goToPage(1);
}

function goToLastPage() {
    // Загружаем последнюю страницу через API, чтобы узнать точное количество страниц
    loadProducts(currentQuery);
    setTimeout(() => {
        if (totalPages > 0) {
            goToPage(totalPages);
        }
    }, 500);
}

// Плавная прокрутка к началу каталога
function scrollToTop() {
    const catalogHeader = document.querySelector('.catalog-header');
    if (catalogHeader) {
        catalogHeader.scrollIntoView({ 
            behavior: 'smooth', 
            block: 'start' 
        });
    }
}

// Показать/скрыть индикатор загрузки
function showLoading(show) {
    const grid = document.getElementById('productsGrid');
    if (!grid) return;
    if (show) {
        grid.style.opacity = '0.6';
        grid.style.pointerEvents = 'none';
    } else {
        grid.style.opacity = '1';
        grid.style.pointerEvents = 'auto';
    }
}

// Обработка Enter в поле поиска
document.getElementById('searchInput')?.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') {
        searchProducts();
    }
});

// Добавление в корзину
async function addToCart(id, name, price) {
    try {
        // Проверяем доступный остаток перед добавлением
        const response = await fetch(`/api/products/${id}`);
        if (!response.ok) {
            alert('Не удалось получить информацию о товаре');
            return;
        }
        
        const product = await response.json();
        const stock = parseInt(product.Stock || product.products_stock || 0);
        
        if (stock <= 0) {
            alert('Товар закончился');
            return;
        }
        
        // Проверяем сколько уже в корзине
        const existingItem = cart.find(item => item.id === id);
        const currentQuantity = existingItem ? existingItem.quantity : 0;
        
        if (currentQuantity >= stock) {
            alert(`Доступно только ${stock} шт. этого товара`);
            return;
        }
        
        if (existingItem) {
            existingItem.quantity += 1;
        } else {
            cart.push({ id, name, price, quantity: 1 });
        }
        saveCart();
        updateCartUI();
        loadProducts(currentQuery); // Перезагружаем для обновления кнопок
    } catch (error) {
        console.error('Error adding to cart:', error);
        alert('Ошибка при добавлении в корзину');
    }
}

// Сохранение корзины
function saveCart() {
    localStorage.setItem('cart', JSON.stringify(cart));
    updateCartCount();
}

// Обновление счетчика корзины
function updateCartCount() {
    const count = cart.reduce((sum, item) => sum + item.quantity, 0);
    const countEl = document.getElementById('cartCount');
    if (countEl) {
        countEl.textContent = count;
        countEl.style.display = count > 0 ? 'flex' : 'none';
    }
}

// Обновление UI корзины
function updateCartUI() {
    const itemsEl = document.getElementById('cartItems');
    const totalEl = document.getElementById('cartTotal');
    
    if (itemsEl) {
        if (cart.length === 0) {
            itemsEl.innerHTML = '<div style="text-align:center;padding:30px;color:#888">Корзина пуста</div>';
        } else {
            itemsEl.innerHTML = cart.map((item, index) => {
                const name = escapeHtml(item.name || '');
                const price = parseFloat(item.price || 0);
                const quantity = parseInt(item.quantity || 0);
                const subtotal = price * quantity;
                return `
                <div style="display:flex;gap:12px;padding:12px 0;border-bottom:1px solid #eee;align-items:center">
                    <div style="flex:1;min-width:0">
                        <div style="font-weight:600;font-size:0.92rem;margin-bottom:2px">${name}</div>
                        <div style="color:#8B6F47;font-weight:700;font-size:0.9rem">${price.toFixed(0)} ₽</div>
                    </div>
                    <div style="display:inline-flex;align-items:center;border:1px solid #ddd;border-radius:5px;overflow:hidden">
                        <button onclick="changeCartQty(${index}, -1)" style="width:30px;height:30px;border:none;background:white;cursor:pointer;font-size:1rem;color:#555">−</button>
                        <span style="min-width:32px;text-align:center;font-weight:600;border-left:1px solid #ddd;border-right:1px solid #ddd;padding:5px 0;font-size:0.9rem">${quantity}</span>
                        <button onclick="changeCartQty(${index}, 1)" style="width:30px;height:30px;border:none;background:white;cursor:pointer;font-size:1rem;color:#555">+</button>
                    </div>
                    <span style="font-weight:700;color:#8B6F47;min-width:70px;text-align:right;font-size:0.9rem">${subtotal.toFixed(0)} ₽</span>
                    <button onclick="removeFromCart(${index})" style="background:none;border:none;color:#ccc;cursor:pointer;font-size:1rem;padding:4px" title="Удалить">✕</button>
                </div>`;
            }).join('');
        }
    }
    
    if (totalEl) {
        const total = cart.reduce((sum, item) => sum + (item.price * item.quantity), 0);
        totalEl.textContent = total.toFixed(0);
    }
}

function changeCartQty(index, delta) {
    if (index < 0 || index >= cart.length) return;
    cart[index].quantity += delta;
    if (cart[index].quantity <= 0) cart.splice(index, 1);
    saveCart();
    updateCartUI();
    loadProducts(currentQuery);
}

// Удаление из корзины
function removeFromCart(index) {
    cart.splice(index, 1);
    saveCart();
    updateCartUI();
    loadProducts();
}

// Показать корзину
function showCart() {
    updateCartUI();
    document.getElementById('cartModal').style.display = 'block';
}

// Закрыть корзину
function closeCart() {
    document.getElementById('cartModal').style.display = 'none';
}

// ======= Выбор времени самовывоза =======
const PICKUP_MONTHS = ['янв','фев','мар','апр','мая','июн','июл','авг','сен','окт','ноя','дек'];

// -1 = закрыто; иначе час закрытия (18 или 16)
function pickupClosingHour(d) {
    if (d.getMonth() === 0 && (d.getDate() === 1 || d.getDate() === 2)) return -1;
    return (d.getDay() === 0 || d.getDay() === 6) ? 16 : 18;
}

function initPickupPicker() {
    const now = new Date();
    // Бронь истекает в 03:00 через 3 календарных дня
    const expDay = new Date(now); expDay.setDate(expDay.getDate() + 3);
    const max = new Date(expDay.getFullYear(), expDay.getMonth(), expDay.getDate(), 3, 0, 0);
    const dateEl = document.getElementById('pickupDate');
    const timeEl = document.getElementById('pickupTime');
    if (!dateEl || !timeEl) return;

    dateEl.innerHTML = '<option value="">— дата —</option>';
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const maxDay = new Date(expDay.getFullYear(), expDay.getMonth(), expDay.getDate());
    const tomorrow = new Date(today.getTime() + 86400000);
    for (let d = new Date(today); d <= maxDay; d = new Date(d.getTime() + 86400000)) {
        if (pickupClosingHour(d) < 0) continue; // 1 и 2 января
        const opt = document.createElement('option');
        opt.value = d.getFullYear() + '-' + String(d.getMonth()+1).padStart(2,'0') + '-' + String(d.getDate()).padStart(2,'0');
        const prefix = d.toDateString() === today.toDateString() ? 'Сегодня, '
            : d.toDateString() === tomorrow.toDateString() ? 'Завтра, ' : '';
        opt.textContent = prefix + d.getDate() + ' ' + PICKUP_MONTHS[d.getMonth()];
        dateEl.appendChild(opt);
    }

    dateEl.onchange = () => updatePickupTimes(now, max);
    timeEl.innerHTML = '<option value="">— время —</option>';
}

function updatePickupTimes(now, max) {
    const dateEl = document.getElementById('pickupDate');
    const timeEl = document.getElementById('pickupTime');
    if (!dateEl || !timeEl) return;
    timeEl.innerHTML = '<option value="">— время —</option>';
    const dateVal = dateEl.value;
    if (!dateVal) return;
    const dayDate = new Date(dateVal + 'T00:00:00');
    const closeH = pickupClosingHour(dayDate);
    if (closeH < 0) { // не рабочий день
        const o = document.createElement('option'); o.textContent = 'выходной — закрыто'; o.disabled = true;
        timeEl.appendChild(o); return;
    }
    const minAllowed = new Date(now.getTime() + 10 * 60 * 1000);
    for (let h = 9; h < 24; h++) {
        for (let m = 0; m < 60; m += 30) {
            if (h * 60 + m > closeH * 60) break;
            const hh = String(h).padStart(2,'0'), mm = String(m).padStart(2,'0');
            const slot = new Date(dateVal + 'T' + hh + ':' + mm + ':00');
            if (slot <= minAllowed || slot > max) continue;
            const opt = document.createElement('option');
            opt.value = slot.toISOString();
            opt.textContent = hh + ':' + mm;
            timeEl.appendChild(opt);
        }
    }
}
// ======= Конец пикера =======

// Оформить заказ
function checkout() {
    if (cart.length === 0) {
        alert('Корзина пуста');
        return;
    }
    closeCart();
    document.getElementById('checkoutModal').style.display = 'block';
    initPickupPicker();
}

// Закрыть форму заказа
function closeCheckout() {
    document.getElementById('checkoutModal').style.display = 'none';
}

// Отправка заказа
document.getElementById('checkoutForm')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    
    const formData = new FormData(e.target);
    const comment = (formData.get('comment') || '').trim() || null;
    
    if (cart.length === 0) {
        alert('Корзина пуста');
        return;
    }
    
    const validItems = cart.filter(item => {
        return item.id > 0 && item.quantity > 0 && item.quantity <= 1000;
    });
    
    if (validItems.length === 0) {
        alert('Нет валидных товаров в корзине');
        return;
    }
    
    const pickupAt = document.getElementById('pickupTime')?.value || null;

    const order = {
        comment: comment,
        pickup_at: pickupAt,
        items: validItems.map(item => ({
            product_id: parseInt(item.id),
            quantity: parseInt(item.quantity)
        }))
    };

    const token = localStorage.getItem('auth_token');
    if (!token) {
        alert('Чтобы оформить заказ, войдите в аккаунт.');
        window.location.href = '/login?redirect=' + encodeURIComponent('/catalog');
        return;
    }

    try {
        const response = await fetch('/api/orders', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + token
            },
            body: JSON.stringify(order)
        });
        
        if (response.ok) {
            alert('Заявка успешно отправлена! Мы свяжемся с вами в ближайшее время.');
            cart = [];
            saveCart();
            updateCartCount();
            closeCheckout();
            loadProducts();
        } else {
            const error = await response.json();
            alert('Ошибка: ' + (error.error || 'Не удалось отправить заявку'));
        }
    } catch (error) {
        console.error('Error submitting order:', error);
        alert('Ошибка при отправке заявки');
    }
});

// Закрытие модальных окон при клике вне их
window.onclick = function(event) {
    const cartModal = document.getElementById('cartModal');
    const checkoutModal = document.getElementById('checkoutModal');
    if (event.target === cartModal) {
        closeCart();
    }
    if (event.target === checkoutModal) {
        closeCheckout();
    }
}

// Инициализация
document.addEventListener('DOMContentLoaded', () => {
    if (!document.getElementById('productsGrid')) return;
    loadProducts(currentQuery);
    updateCartCount();
    
    // Добавляем клавиатурную навигацию
    document.addEventListener('keydown', (e) => {
        // Только если не в поле ввода
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
        
        switch(e.key) {
            case 'ArrowLeft':
                e.preventDefault();
                if (currentPage > 1) prevPage();
                break;
            case 'ArrowRight':
                e.preventDefault();
                if (currentPage < totalPages) nextPage();
                break;
            case 'Home':
                e.preventDefault();
                goToFirstPage();
                break;
            case 'End':
                e.preventDefault();
                goToLastPage();
                break;
        }
    });
    
    // Добавляем обработку для динамических элементов пагинации
    document.addEventListener('click', (e) => {
        if (e.target.matches('.pagination-controls button')) {
            const button = e.target;
            if (button.disabled) {
                e.preventDefault();
                return;
            }
        }
    });

    // Загружаем категории
    loadCategories();
});

// =============================================================================
// КАТЕГОРИИ: горизонтальная панель
// =============================================================================
let allCategories = [];
let childrenByParent = new Map();

async function loadCategories() {
    try {
        const resp = await fetch('/api/categories');
        const data = await resp.json();
        if (!data.categories || !data.categories.length) return;

        allCategories = data.categories;
        childrenByParent = new Map();
        allCategories.forEach(cat => {
            const key = cat.parent_id ? String(cat.parent_id) : 'root';
            if (!childrenByParent.has(key)) childrenByParent.set(key, []);
            childrenByParent.get(key).push(cat);
        });

        for (const [key, list] of childrenByParent) {
            list.sort((a, b) => a.name.localeCompare(b.name, 'ru'));
        }

        renderCategoryBar();
    } catch (e) {
        console.error('Failed to load categories:', e);
    }
}

function renderCategoryBar() {
    const bar = document.getElementById('categoryBar');
    if (!bar) return;

    const roots = childrenByParent.get('root') || [];
    let html = `<button class="cat-pill${currentCategoryId === '' ? ' active' : ''}" data-category-id="" onclick="selectCategory(event, '')">Все</button>`;

    roots.forEach(cat => {
        const hasChildren = childrenByParent.has(String(cat.id));
        const arrow = hasChildren ? '<span class="pill-arrow">▾</span>' : '';
        const active = currentCategoryId === String(cat.id) || (hasChildren && currentCategoryId && allCategories.find(c => String(c.id) === currentCategoryId)?.parent_id == cat.id);
        html += `<button class="cat-pill${active ? ' active' : ''}" data-category-id="${cat.id}" onclick="selectCategory(event, '${cat.id}')">${escapeHtml(cat.name)}${arrow}</button>`;
    });

    bar.innerHTML = html;
}

function renderSubcategoryBar(parentId) {
    const sub = document.getElementById('subcategoryBar');
    if (!sub) return;

    const children = childrenByParent.get(String(parentId)) || [];
    if (children.length === 0) {
        sub.innerHTML = '';
        sub.hidden = true;
        return;
    }

    let html = `<button class="cat-pill${currentCategoryId === String(parentId) ? ' active' : ''}" data-category-id="${parentId}" onclick="selectCategory(event, '${parentId}')">Все в категории</button>`;
    children.forEach(cat => {
        html += `<button class="cat-pill${currentCategoryId === String(cat.id) ? ' active' : ''}" data-category-id="${cat.id}" onclick="selectCategory(event, '${cat.id}')">${escapeHtml(cat.name)}</button>`;
    });

    sub.innerHTML = html;
    sub.hidden = false;
}

function selectCategory(event, categoryId) {
    event.preventDefault();
    const idStr = String(categoryId);

    currentCategoryId = idStr;
    currentOffset = 0;
    currentQuery = '';
    const input = document.getElementById('searchInput');
    if (input) input.value = '';

    // Показываем/скрываем подкатегории
    if (idStr === '') {
        document.getElementById('subcategoryBar').hidden = true;
    } else {
        const cat = allCategories.find(c => String(c.id) === idStr);
        if (cat && !cat.parent_id) {
            // Это корневая категория — показываем её подкатегории
            renderSubcategoryBar(idStr);
        } else if (cat && cat.parent_id) {
            // Это подкатегория — показываем соседей
            renderSubcategoryBar(String(cat.parent_id));
        } else {
            document.getElementById('subcategoryBar').hidden = true;
        }
    }

    renderCategoryBar();
    updateBreadcrumb();
    loadProducts();
}

function updateBreadcrumb() {
    const bc = document.getElementById('breadcrumb');
    if (!bc) return;

    if (!currentCategoryId) {
        bc.innerHTML = '<span class="breadcrumb-item active">Все товары</span>';
        return;
    }

    const cat = allCategories.find(c => String(c.id) === currentCategoryId);
    if (!cat) {
        bc.innerHTML = '<span class="breadcrumb-item active">Все товары</span>';
        return;
    }

    let html = '';
    if (cat.parent_id) {
        const parent = allCategories.find(c => String(c.id) === String(cat.parent_id));
        if (parent) {
            html += `<span class="breadcrumb-item" onclick="selectCategory(event, '${parent.id}')">${escapeHtml(parent.name)}</span>`;
            html += '<span class="breadcrumb-sep">›</span>';
        }
    }
    html += `<span class="breadcrumb-item active">${escapeHtml(cat.name)}</span>`;
    bc.innerHTML = html;
}


