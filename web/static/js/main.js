// ХозДача — общие скрипты главной
function escapeHtml(text) {
    if (!text) return '';
    const map = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' };
    return String(text).replace(/[&<>"']/g, m => map[m]);
}

function formatPrice(n) {
    return parseFloat(n || 0).toLocaleString('ru-RU', { maximumFractionDigits: 0 });
}

function stockClass(stock) {
    const s = parseInt(stock || 0);
    if (s <= 0) return 'product-card__stock product-card__stock--out';
    if (s < 5) return 'product-card__stock product-card__stock--low';
    return 'product-card__stock product-card__stock--ok';
}

function stockLabel(stock) {
    const s = parseInt(stock || 0);
    if (s <= 0) return 'Нет в наличии';
    if (s < 5) return `Осталось ${s} шт.`;
    return `В наличии: ${s} шт.`;
}

async function loadPromotions() {
    const grid = document.getElementById('promotionsGrid');
    if (!grid) return;
    try {
        const response = await fetch('/api/promotions');
        const data = await response.json();
        if (data.promotions && data.promotions.length > 0) {
            grid.innerHTML = data.promotions.map(promo => {
                const title = escapeHtml(promo.promotions_title || 'Акция');
                const desc = promo.promotions_description ? escapeHtml(promo.promotions_description) : '';
                const discount = parseFloat(promo.promotions_discount || 0);
                return `
                <div class="promo-card">
                    ${discount > 0 ? `<span class="promo-card__percent">−${Math.round(discount)}%</span>` : ''}
                    <div class="promo-card__title">${title}</div>
                    ${desc ? `<p class="promo-card__desc">${desc}</p>` : ''}
                </div>`;
            }).join('');
        } else {
            grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><div class="empty-state__icon">🎁</div><h3>Сейчас акций нет</h3><p>Загляните позже — мы готовим новые предложения</p></div>';
        }
    } catch (error) {
        console.error('Error loading promotions:', error);
        grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>Не удалось загрузить акции</p></div>';
    }
}

async function loadProducts() {
    const grid = document.getElementById('productsGrid');
    if (!grid) return;
    try {
        const response = await fetch('/api/products?limit=10');
        const data = await response.json();
        if (data.products && data.products.length > 0) {
            grid.innerHTML = data.products.map(product => {
                const name = escapeHtml(product.products_name || 'Товар');
                const desc = product.products_description ? escapeHtml(product.products_description.substring(0, 90)) : '';
                const image = product.products_image_url ? escapeHtml(product.products_image_url) : '';
                const price = parseFloat(product.products_price || 0);
                const stock = product.products_stock || product.stock || 0;
                const hasPromo = product.promo_price && parseFloat(product.promo_price) < price;
                const finalPrice = hasPromo ? parseFloat(product.promo_price) : price;
                return `
                <a class="product-card" href="/product/${product.id}">
                    <div class="product-card__media">
                        ${image
                            ? `<img src="${image}" alt="${name}" loading="lazy" onerror="this.replaceWith(Object.assign(document.createElement('div'),{className:'product-card__media--empty',textContent:'Нет фото'}))">`
                            : '<div class="product-card__media--empty">Нет фото</div>'}
                        ${hasPromo ? `<span class="product-card__badge">−${Math.round((1 - product.promo_price / price) * 100)}%</span>` : ''}
                    </div>
                    <div class="product-card__body">
                        <div class="product-card__title">${name}</div>
                        ${desc ? `<div class="product-card__desc">${desc}…</div>` : ''}
                        <div class="product-card__price-row">
                            ${hasPromo ? `<span class="product-card__price--old">${formatPrice(price)} ₽</span>` : ''}
                            <span class="product-card__price${hasPromo ? ' product-card__price--new' : ''}">${formatPrice(finalPrice)} ₽</span>
                        </div>
                        <span class="${stockClass(stock)}">${stockLabel(stock)}</span>
                    </div>
                </a>`;
            }).join('');
        } else {
            grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><div class="empty-state__icon">📦</div><h3>Товаров пока нет</h3><p>Скоро здесь появятся новинки</p></div>';
        }
    } catch (error) {
        console.error('Error loading products:', error);
        grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1"><p>Не удалось загрузить товары</p></div>';
    }
}

document.addEventListener('DOMContentLoaded', () => {
    loadPromotions();
    loadProducts();
});
