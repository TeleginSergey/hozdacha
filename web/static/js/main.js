// Загрузка акций на главной странице
async function loadPromotions() {
    try {
        const response = await fetch('/api/promotions');
        const data = await response.json();
        const grid = document.getElementById('promotionsGrid');
        
        if (data.promotions && data.promotions.length > 0) {
            grid.innerHTML = data.promotions.map(promotion => {
                const title = escapeHtml(promotion.promotions_title || '');
                const desc = promotion.promotions_description ? escapeHtml(promotion.promotions_description) : '';
                const image = promotion.promotions_image_url ? escapeHtml(promotion.promotions_image_url) : '';
                const discount = promotion.promotions_discount || 0;
                
                return `
                <div class="promotion-card">
                    ${image ? `<img src="${image}" alt="${title}" onerror="this.style.display='none'">` : ''}
                    <h3>${title}</h3>
                    ${desc ? `<p>${desc}</p>` : ''}
                    ${discount > 0 ? `<p class="discount">Скидка: ${discount}%</p>` : ''}
                </div>
            `;
            }).join('');
        } else {
            grid.innerHTML = '<p>Акций пока нет</p>';
        }
    } catch (error) {
        console.error('Error loading promotions:', error);
        document.getElementById('promotionsGrid').innerHTML = '<p>Ошибка загрузки акций</p>';
    }
}

// Загрузка популярных товаров
async function loadProducts() {
    try {
        const response = await fetch('/api/products?limit=6');
        const data = await response.json();
        const grid = document.getElementById('productsGrid');
        
        if (data.products && data.products.length > 0) {
            grid.innerHTML = data.products.map(product => {
                const name = escapeHtml(product.products_name || '');
                const desc = product.products_description ? escapeHtml(product.products_description.substring(0, 100)) + '...' : '';
                const image = product.products_image_url ? escapeHtml(product.products_image_url) : '';
                const price = parseFloat(product.products_price || 0).toFixed(2);
                
                return `
                <div class="product-card">
                    ${image ? `<img src="${image}" alt="${name}" onerror="this.style.display='none'">` : ''}
                    <h3>${name}</h3>
                    ${desc ? `<p>${desc}</p>` : ''}
                    <p class="price">${price} руб.</p>
                    <a href="/catalog" class="btn">Подробнее</a>
                </div>
            `;
            }).join('');
        } else {
            grid.innerHTML = '<p>Товаров пока нет</p>';
        }
    } catch (error) {
        console.error('Error loading products:', error);
        document.getElementById('productsGrid').innerHTML = '<p>Ошибка загрузки товаров</p>';
    }
}

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

// Инициализация при загрузке страницы
document.addEventListener('DOMContentLoaded', () => {
    loadPromotions();
    loadProducts();
});


