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

// Бесконечная подгрузка
const pageLimit = 25;
let currentOffset = 0;
let currentQuery = '';
let currentCategoryId = '';
let hasMoreProducts = true;
let isLoadingProducts = false;
let infiniteScrollDisconnect = null;

function resetCatalogList() {
    currentOffset = 0;
    hasMoreProducts = true;
}

function renderProductCardsHTML(products) {
    if (!window.ProductCard) return '';
    return products.map(function(p) {
        return window.ProductCard.render(window.ProductCard.read(p), { showAddToCart: true });
    }).join('');
}

// Загрузка товаров (append = догрузка при скролле)
async function loadProducts(query = '', append = false) {
    if (isLoadingProducts) return;
    if (append && !hasMoreProducts) return;

    const grid = document.getElementById('productsGrid');
    const loader = document.getElementById('catalogScrollLoader');
    const sentinel = document.getElementById('catalogScrollSentinel');
    if (!grid) return;

    isLoadingProducts = true;
    if (!append) {
        showLoading(true);
        resetCatalogList();
    } else if (loader) {
        loader.hidden = false;
    }

    try {
        let url;
        if (query) {
            url = '/api/products/search?q=' + encodeURIComponent(query) + '&limit=' + pageLimit + '&offset=' + currentOffset;
            if (currentCategoryId) {
                url += '&category_id=' + encodeURIComponent(currentCategoryId);
            }
        } else {
            url = '/api/products?limit=' + pageLimit + '&offset=' + currentOffset;
            if (currentCategoryId) {
                url += '&category_id=' + encodeURIComponent(currentCategoryId);
            }
        }

        const response = await fetch(url);
        const data = await response.json();
        const products = (data && data.products) || [];

        if (products.length > 0) {
            const html = renderProductCardsHTML(products);
            if (append) {
                grid.insertAdjacentHTML('beforeend', html);
            } else {
                grid.innerHTML = html;
            }
            currentOffset += products.length;
            hasMoreProducts = data.has_more !== undefined ? !!data.has_more : products.length >= pageLimit;
        } else if (!append) {
            grid.innerHTML = '<div class="no-products" style="grid-column:1/-1"><p>Товары не найдены</p></div>';
            hasMoreProducts = false;
        } else {
            hasMoreProducts = false;
        }

        if (!hasMoreProducts && sentinel) {
            sentinel.style.display = 'none';
        } else if (sentinel) {
            sentinel.style.display = 'block';
        }
    } catch (error) {
        console.error('Error loading products:', error);
        if (!append) {
            grid.innerHTML = '<div class="error-message" style="grid-column:1/-1"><p>Ошибка загрузки товаров</p></div>';
        }
        hasMoreProducts = false;
    } finally {
        isLoadingProducts = false;
        showLoading(false);
        if (loader) loader.hidden = true;
    }
}

function setupCatalogInfiniteScroll() {
    const sentinel = document.getElementById('catalogScrollSentinel');
    if (!sentinel || !window.InfiniteScroll) return;
    if (infiniteScrollDisconnect) infiniteScrollDisconnect();
    infiniteScrollDisconnect = window.InfiniteScroll.observe(sentinel, function() {
        return loadProducts(currentQuery, true);
    });
}

// Поиск товаров
function searchProducts() {
    const query = document.getElementById('searchInput').value;
    currentQuery = (query || '').trim();
    resetCatalogList();
    const sentinel = document.getElementById('catalogScrollSentinel');
    if (sentinel) sentinel.style.display = 'block';

    // Читаем scope: если выбрана категория — ищем только в ней
    const scope = document.getElementById('searchScope');
    if (scope && scope.value) {
        currentCategoryId = scope.value;
        activeCategoryId = scope.value;
        const cat = allCategories.find(c => String(c.id) === scope.value);
        activeParentId = cat && cat.parent_id ? String(cat.parent_id) : (cat ? scope.value : '');
    } else {
        currentCategoryId = '';
        activeCategoryId = '';
        activeParentId = '';
    }

    renderCategoryTree();
    updateBreadcrumb();
    updateSearchScope(activeCategoryId);
    renderActiveFilters();

    if (!currentQuery) {
        loadProducts();
        return;
    }

    loadProducts(currentQuery);
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
            alert('Не удалось загрузить товар');
            return;
        }
        
        const product = await response.json();
        const stock = parseInt(product.Stock || product.products_stock || 0);
        
        if (stock <= 0) {
            alert('Этого товара сейчас нет на складе. Выберите другой товар.');
            return;
        }
        
        // Проверяем сколько уже в корзине
        const existingItem = cart.find(item => item.id === id);
        const currentQuantity = existingItem ? existingItem.quantity : 0;
        
        if (currentQuantity >= stock) {
            alert('Вы уже добавили всё, что есть на складе. Доступно всего ' + stock + ' шт.');
            return;
        }
        
        if (existingItem) {
            existingItem.quantity += 1;
        } else {
            // Сохраняем данные акции, чтобы показывать старую цену и скидку в корзине.
            const base = parseFloat(product.Price ?? product.products_price ?? price);
            const effRaw = product.EffectivePrice ?? product.effective_price;
            const eff = (effRaw !== undefined && effRaw !== null) ? parseFloat(effRaw) : null;
            const disc = parseFloat(product.DiscountPercent ?? product.discount_percent ?? 0);
            const hasPromo = eff !== null && eff < base;
            // Новые товары — наверх корзины (unshift), как в Ozon/WB.
            cart.unshift({
                id, name,
                price: hasPromo ? eff : base,
                oldPrice: hasPromo ? base : null,
                discountPercent: hasPromo ? Math.round(disc) : 0,
                quantity: 1,
                image: product.ImageURL || product.products_image_url || '',
                selected: true
            });
        }
        saveCart();
        updateCartUI();
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
                const img = item.image || '';
                return `
                <div style="display:flex;gap:12px;padding:12px 0;border-bottom:1px solid #eee;align-items:center">
                    ${img ? `<img src="${escapeHtml(img)}" alt="" style="width:56px;height:56px;border-radius:6px;object-fit:cover;flex-shrink:0;background:#f5f5f5" onerror="this.style.display='none'">` : ''}
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
    if (delta > 0) {
        // Проверяем остаток на складе перед увеличением
        const item = cart[index];
        fetch(`/api/products/${item.id}`)
            .then(r => r.json())
            .then(product => {
                const stock = parseInt(product.Stock || product.products_stock || 0);
                if (item.quantity >= stock) {
                    alert('На складе больше нет этого товара. Доступно всего ' + stock + ' шт.');
                    return;
                }
                item.quantity += delta;
                saveCart();
                updateCartUI();
            })
            .catch(() => alert('Не удалось проверить наличие товара'));
        return;
    }
    cart[index].quantity += delta;
    if (cart[index].quantity <= 0) cart.splice(index, 1);
    saveCart();
    updateCartUI();
}

// Удаление из корзины
function removeFromCart(index) {
    cart.splice(index, 1);
    saveCart();
    updateCartUI();
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
        alert('В корзине нет товаров');
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
        alert('Для оформления заказа нужно войти');
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
            alert('Заказ принят! Мы с вами свяжемся.');
            cart = [];
            saveCart();
            updateCartCount();
            closeCheckout();
        } else {
            const error = await response.json();
            alert('Ошибка: ' + (error.error || 'Не удалось оформить заказ'));
        }
    } catch (error) {
        console.error('Error submitting order:', error);
        alert('Ошибка при оформлении заказа');
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

    if (window.ProductCard) {
        window.ProductCard.onAddToCart = addToCart;
    }

    loadProducts(currentQuery).then(function() {
        setupCatalogInfiniteScroll();
    });
    updateCartCount();

    // Загружаем категории
    loadCategories();

    // Мобильная полоса: поиск.
    const mInput = document.getElementById('mSearchInput');
    if (mInput) {
        mInput.addEventListener('keydown', function(e) {
            if (e.key !== 'Enter') return;
            const main = document.getElementById('searchInput');
            if (main) main.value = mInput.value;
            searchProducts();
        });
    }
});

// =============================================================================
// КАТЕГОРИИ: дерево в sidebar
// =============================================================================
let allCategories = [];
let childrenByParent = new Map();
let activeCategoryId = '';
let activeParentId = '';

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

        renderCategoryTree();
        renderActiveFilters();
    } catch (e) {
        console.error('Failed to load categories:', e);
    }
}

function renderCategoryTree(filterText) {
    const tree = document.getElementById('categoryTree');
    if (!tree) return;

    const roots = childrenByParent.get('root') || [];
    const ft = (filterText || '').toLowerCase().trim();

    let html = '';

    const allMatch = !ft || 'все товары'.includes(ft);
    html += `<div class="tree-item${activeCategoryId === '' ? ' active' : ''}${allMatch ? '' : ' hidden-by-filter'}" data-category-id="" onclick="selectTreeCategory(event, '')">
        <span class="tree-icon leaf">▸</span>
        <span class="tree-label">Все товары</span>
    </div>`;

    roots.forEach(cat => {
        const hasChildren = childrenByParent.has(String(cat.id));
        const children = childrenByParent.get(String(cat.id)) || [];
        const catMatch = !ft || cat.name.toLowerCase().includes(ft);
        const childMatch = !ft || children.some(c => c.name.toLowerCase().includes(ft));
        const visible = catMatch || childMatch;

        if (!visible) return;

        const isActive = activeCategoryId === String(cat.id);
        const isParentActive = activeParentId === String(cat.id);
        const expanded = isActive || isParentActive || (!!ft && childMatch);

        html += `<div class="tree-item${isActive ? ' active' : ''}${expanded ? ' expanded' : ''}" data-category-id="${cat.id}" onclick="selectTreeCategory(event, '${cat.id}')">
            <span class="tree-icon">▸</span>
            <span class="tree-label">${escapeHtml(cat.name)}</span>
        </div>`;

        if (hasChildren) {
            html += '<div class="tree-children' + (expanded ? '' : ' collapsed') + '" style="max-height:' + (expanded ? (children.length * 36) + 'px' : '0') + '">';
            children.forEach(sub => {
                const subMatch = !ft || sub.name.toLowerCase().includes(ft);
                if (!subMatch && ft) return;
                const subActive = activeCategoryId === String(sub.id);
                html += `<div class="tree-item${subActive ? ' active' : ''}" data-category-id="${sub.id}" onclick="selectTreeCategory(event, '${sub.id}')">
                    <span class="tree-icon leaf">▸</span>
                    <span class="tree-label">${escapeHtml(sub.name)}</span>
                </div>`;
            });
            html += '</div>';
        }
    });

    if (!ft && roots.length === 0) {
        html += '<div class="tree-loading">Нет категорий</div>';
    }

    tree.innerHTML = html;
}

function selectTreeCategory(event, categoryId) {
    event.stopPropagation();
    const idStr = String(categoryId);

    if (idStr === '') {
        activeCategoryId = '';
        activeParentId = '';
    } else {
        const cat = allCategories.find(c => String(c.id) === idStr);
        if (!cat) return;

        const hasChildren = childrenByParent.has(idStr);
        if (hasChildren) {
            const treeItem = event.currentTarget;
            const wasExpanded = treeItem.classList.contains('expanded');
            if (wasExpanded) {
                treeItem.classList.remove('expanded');
                const childrenDiv = treeItem.nextElementSibling;
                if (childrenDiv && childrenDiv.classList.contains('tree-children')) {
                    childrenDiv.classList.add('collapsed');
                    childrenDiv.style.maxHeight = '0';
                }
            } else {
                treeItem.classList.add('expanded');
                const childrenDiv = treeItem.nextElementSibling;
                if (childrenDiv && childrenDiv.classList.contains('tree-children')) {
                    childrenDiv.classList.remove('collapsed');
                    const count = childrenDiv.querySelectorAll('.tree-item').length;
                    childrenDiv.style.maxHeight = (count * 36) + 'px';
                }
            }
            activeCategoryId = idStr;
            activeParentId = idStr;
        } else {
            activeCategoryId = idStr;
            activeParentId = cat.parent_id ? String(cat.parent_id) : '';
        }
    }

    renderCategoryTree();
    updateBreadcrumb();
    updateSearchScope(activeCategoryId);
    renderActiveFilters();

    currentCategoryId = activeCategoryId;
    currentQuery = '';
    const input = document.getElementById('searchInput');
    if (input) input.value = '';
    const sentinel = document.getElementById('catalogScrollSentinel');
    if (sentinel) sentinel.style.display = 'block';
    loadProducts();

    // На телефоне закрываем выезжающую панель после выбора конечной категории / «Все»
    // (для раскрытия родителя с подкатегориями оставляем открытой).
    if (window.innerWidth <= 900) {
        const isLeaf = idStr === '' || !childrenByParent.has(idStr);
        if (isLeaf) {
            const sb = document.getElementById('catalogSidebar');
            if (sb) sb.classList.add('collapsed');
        }
    }
}

// Выбор категории напрямую по id (крестик активного фильтра → сброс на «Все»).
function chooseCategory(id) {
    const idStr = id ? String(id) : '';
    if (idStr === '') {
        activeCategoryId = '';
        activeParentId = '';
    } else {
        const cat = allCategories.find(c => String(c.id) === idStr);
        if (!cat) return;
        activeCategoryId = idStr;
        activeParentId = cat.parent_id ? String(cat.parent_id) : idStr;
    }
    renderCategoryTree();
    updateBreadcrumb();
    updateSearchScope(activeCategoryId);
    renderActiveFilters();

    currentCategoryId = activeCategoryId;
    // Поисковый запрос сохраняем: при снятии категории поиск продолжает
    // действовать в новом скоупе (выбранная категория или все товары).
    const sentinel = document.getElementById('catalogScrollSentinel');
    if (sentinel) sentinel.style.display = 'block';
    loadProducts(currentQuery);
}

// Активные фильтры (выбранная категория и/или поиск) — чипы с крестиком.
// Рендерим во все контейнеры: мобильная липкая полоса и десктопный топбар.
function renderActiveFilters() {
    const boxes = document.querySelectorAll('.js-cat-active');
    if (!boxes.length) return;
    const xSvg = '<span class="filter-chip__x"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg></span>';
    let html = '';
    if (activeCategoryId) {
        const cat = allCategories.find(c => String(c.id) === String(activeCategoryId));
        if (cat) {
            html += `<button type="button" class="filter-chip" onclick="chooseCategory('')"><span>${escapeHtml(cat.name)}</span>${xSvg}</button>`;
        }
    }
    if (currentQuery) {
        html += `<button type="button" class="filter-chip" onclick="clearCatalogSearch()"><span>Поиск: ${escapeHtml(currentQuery)}</span>${xSvg}</button>`;
    }
    boxes.forEach(function(box) { box.innerHTML = html; box.hidden = !html; });
}

// Сброс только поискового запроса (крестик на чипе поиска).
function clearCatalogSearch() {
    currentQuery = '';
    const main = document.getElementById('searchInput');
    if (main) main.value = '';
    const mInput = document.getElementById('mSearchInput');
    if (mInput) mInput.value = '';
    resetCatalogList();
    const sentinel = document.getElementById('catalogScrollSentinel');
    if (sentinel) sentinel.style.display = 'block';
    loadProducts(currentQuery);
    renderActiveFilters();
}

function updateBreadcrumb() {
    const bc = document.getElementById('breadcrumb');
    if (!bc) return;

    if (!activeCategoryId) {
        bc.innerHTML = '<span class="breadcrumb-item active">Все товары</span>';
        return;
    }

    const cat = allCategories.find(c => String(c.id) === activeCategoryId);
    if (!cat) {
        bc.innerHTML = '<span class="breadcrumb-item active">Все товары</span>';
        return;
    }

    let html = '';
    if (cat.parent_id) {
        const parent = allCategories.find(c => String(c.id) === String(cat.parent_id));
        if (parent) {
            html += `<span class="breadcrumb-item" onclick="selectTreeCategory(event, '${parent.id}')">${escapeHtml(parent.name)}</span>`;
            html += '<span class="breadcrumb-sep">›</span>';
        }
    }
    html += `<span class="breadcrumb-item active">${escapeHtml(cat.name)}</span>`;
    bc.innerHTML = html;
}

function updateSearchScope(categoryId) {
    const scope = document.getElementById('searchScope');
    if (!scope) return;

    if (!categoryId) {
        scope.innerHTML = '<option value="">Везде</option>';
        return;
    }

    const cat = allCategories.find(c => String(c.id) === categoryId);
    if (!cat) {
        scope.innerHTML = '<option value="">Везде</option>';
        return;
    }

    scope.innerHTML = '<option value="">Везде</option>' +
        '<option value="' + categoryId + '" selected>В категории «' + escapeHtml(cat.name) + '»</option>';
}

function filterCategoryTree() {
    const input = document.getElementById('sidebarSearch');
    const ft = (input?.value || '').trim();
    renderCategoryTree(ft);
}

function toggleSidebar() {
    const sidebar = document.getElementById('catalogSidebar');
    if (!sidebar) return;
    sidebar.classList.toggle('collapsed');
}

document.addEventListener('click', function(e) {
    const sidebar = document.getElementById('catalogSidebar');
    if (!sidebar || sidebar.classList.contains('collapsed')) return;
    if (window.innerWidth > 768) return;
    if (!sidebar.contains(e.target) && !e.target.closest('.sidebar-open-btn') && !e.target.closest('.catbar-cats-btn')) {
        sidebar.classList.add('collapsed');
    }
});


