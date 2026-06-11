// ХозДача — общие скрипты главной страницы.
// Бэкенд отдаёт товары и акции в camelCase: Name, Price, ImageURL, Stock, ID,
// EffectivePrice, DiscountPercent, PromotionTitle, PromotionType, promotions_title и т.д.
function escapeHtml(text) {
    if (text === null || text === undefined) return '';
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

// Универсальный парсер полей товара: бэкенд отдаёт camelCase, но
// на всякий случай поддерживаем и snake_case-совместимость.
function pickField(obj, camelName, snakeName) {
    if (!obj) return undefined;
    if (obj[camelName] !== undefined && obj[camelName] !== null) return obj[camelName];
    if (snakeName && obj[snakeName] !== undefined && obj[snakeName] !== null) return obj[snakeName];
    return undefined;
}

function readProduct(product) {
    const id = pickField(product, 'ID', 'id') || pickField(product, 'ProductsID', 'products_id_pk');
    const name = pickField(product, 'Name', 'name') || pickField(product, 'ProductsName', 'products_name') || 'Товар';
    const description = pickField(product, 'Description', 'description') || pickField(product, 'ProductsDescription', 'products_description');
    const price = parseFloat(pickField(product, 'Price', 'price') || pickField(product, 'ProductsPrice', 'products_price') || 0);
    const image = pickField(product, 'ImageURL', 'image_url') || pickField(product, 'ProductsImageURL', 'products_image_url');
    const stock = parseInt(pickField(product, 'Stock', 'stock') || pickField(product, 'ProductsStock', 'products_stock') || 0);
    const effective = pickField(product, 'EffectivePrice', 'effective_price');
    const discountPercent = pickField(product, 'DiscountPercent', 'discount_percent');
    const hasPromo = effective !== undefined && effective !== null
        ? parseFloat(effective) < price
        : false;
    return {
        id: id,
        name: name,
        description: description,
        price: price,
        image: image,
        stock: stock,
        effectivePrice: hasPromo ? parseFloat(effective) : null,
        discountPercent: hasPromo ? parseFloat(discountPercent || 0) : 0,
    };
}

function renderProductCard(p) {
    const name = escapeHtml(p.name);
    const desc = p.description ? escapeHtml(String(p.description).substring(0, 90)) : '';
    const image = p.image ? escapeHtml(p.image) : '';
    const finalPrice = p.effectivePrice !== null ? p.effectivePrice : p.price;
    const badge = p.effectivePrice !== null
        ? `<span class="product-card__badge">−${Math.round(p.discountPercent)}%</span>`
        : '';
    const media = image
        ? `<img src="${image}" alt="${name}" loading="lazy" onerror="this.replaceWith(Object.assign(document.createElement('div'),{className:'product-card__media--empty',textContent:'Нет фото'}))">`
        : '<div class="product-card__media--empty">Нет фото</div>';
    const priceRow = p.effectivePrice !== null
        ? `<div class="product-card__price-row">
                <span class="product-card__price--old">${formatPrice(p.price)} ₽</span>
                <span class="product-card__price product-card__price--new">${formatPrice(finalPrice)} ₽</span>
           </div>`
        : `<div class="product-card__price-row">
                <span class="product-card__price">${formatPrice(finalPrice)} ₽</span>
           </div>`;

    return `
        <a class="product-card" href="/product/${p.id}">
            <div class="product-card__media">
                ${media}
                ${badge}
            </div>
            <div class="product-card__body">
                <div class="product-card__title">${name}</div>
                ${desc ? `<div class="product-card__desc">${desc}…</div>` : ''}
                ${priceRow}
                <span class="${stockClass(p.stock)}">${stockLabel(p.stock)}</span>
            </div>
        </a>`;
}

async function loadPromotions() {
    const grid = document.getElementById('promotionsGrid');
    if (!grid) return;
    try {
        const response = await fetch('/api/promotions');
        const data = await response.json();
        const promos = (data && data.promotions) || [];
        if (promos.length > 0) {
            grid.innerHTML = promos.map(promo => {
                // Поля API: promotions_title, promotions_description, promotions_discount, promotions_image_url
                const title = escapeHtml(promo.promotions_title || promo.Title || 'Акция');
                const desc = (promo.promotions_description || promo.Description)
                    ? escapeHtml(promo.promotions_description || promo.Description)
                    : '';
                const discount = parseFloat(promo.promotions_discount || promo.Discount || 0);
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
        const raw = (data && data.products) || [];
        if (raw.length > 0) {
            const cards = raw.map(readProduct).map(renderProductCard).join('');
            grid.innerHTML = cards;
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
