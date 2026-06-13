// ХозДача — общий рендер карточки товара.
// Используется и на главной, и на /promotions, и в /catalog.
// Принимает «сырой» объект из бэкенда (camelCase или snake_case — поддерживаем оба),
// отдаёт HTML-строку <a class="product-card">.
(function() {
    'use strict';

    function escapeHtml(text) {
        if (text === null || text === undefined) return '';
        const map = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' };
        return String(text).replace(/[&<>"']/g, m => map[m]);
    }

    function formatPrice(n) {
        return parseFloat(n || 0).toLocaleString('ru-RU', { maximumFractionDigits: 0 });
    }

    function pickField(obj, camelName, snakeName) {
        if (!obj) return undefined;
        if (obj[camelName] !== undefined && obj[camelName] !== null) return obj[camelName];
        if (snakeName && obj[snakeName] !== undefined && obj[snakeName] !== null) return obj[snakeName];
        return undefined;
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
        if (s < 5) return 'Осталось ' + s + ' шт.';
        return 'В наличии: ' + s + ' шт.';
    }

    // Глобально экспортируем, чтобы main.js / promotions.js / catalog.js не дублировали.
    window.ProductCard = {
        escapeHtml: escapeHtml,
        formatPrice: formatPrice,
        pickField: pickField,
        stockClass: stockClass,
        stockLabel: stockLabel,

        // Нормализованное представление из бэкенда.
        read: function(product) {
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
                promotionTitle: pickField(product, 'PromotionTitle', 'promotion_title') || '',
                promotionType: pickField(product, 'PromotionType', 'promotion_type') || '',
            };
        },

        // Рендер карточки. Опции:
        //   showAddToCart — добавить кнопку «В корзину» (используется на /catalog).
        //   onAddToCart   — функция(productId, name, price), вызывается при клике.
        render: function(p, opts) {
            opts = opts || {};
            const name = escapeHtml(p.name);
            const desc = p.description ? escapeHtml(String(p.description).substring(0, 90)) : '';
            const image = p.image ? escapeHtml(p.image) : '';
            const finalPrice = p.effectivePrice !== null ? p.effectivePrice : p.price;
            const badge = p.effectivePrice !== null
                ? '<span class="product-card__badge">−' + Math.round(p.discountPercent) + '%</span>'
                : '';
            const promoLabel = (opts.showPromoLabel && p.promotionTitle)
                ? '<div class="product-card__promo-label">' + escapeHtml(p.promotionTitle) + '</div>'
                : '';
            const media = image
                ? '<img src="' + image + '" alt="' + name + '" loading="lazy" '
                    + 'onerror="this.replaceWith(Object.assign(document.createElement(\'div\'),'
                    + '{className:\'product-card__media--empty\',textContent:\'Нет фото\'}))">'
                : '<div class="product-card__media--empty">Нет фото</div>';
            const priceRow = p.effectivePrice !== null
                ? '<div class="product-card__price-row">'
                    + '<span class="product-card__price--old">' + formatPrice(p.price) + ' ₽</span>'
                    + '<span class="product-card__price product-card__price--new">' + formatPrice(finalPrice) + ' ₽</span>'
                    + '</div>'
                : '<div class="product-card__price-row">'
                    + '<span class="product-card__price">' + formatPrice(finalPrice) + ' ₽</span>'
                    + '</div>';

            const addBtn = opts.showAddToCart
                ? '<button class="btn btn--block btn--sm" style="margin-top:8px"'
                    + ' onclick="event.preventDefault(); event.stopPropagation();'
                    + ' window.ProductCard.onAddToCart && window.ProductCard.onAddToCart('
                    + p.id + ', \'' + name.replace(/'/g, "\\'") + '\', ' + finalPrice + '); return false;">'
                    + 'В корзину</button>'
                : '';

            return ''
                + '<a class="product-card" href="/product/' + p.id + '">'
                +   '<div class="product-card__media">' + media + badge + '</div>'
                +   '<div class="product-card__body">'
                +     '<div class="product-card__title">' + name + '</div>'
                +     promoLabel
                +     (desc ? '<div class="product-card__desc">' + desc + '…</div>' : '')
                +     priceRow
                +     '<span class="' + stockClass(p.stock) + '">' + stockLabel(p.stock) + '</span>'
                +     addBtn
                +   '</div>'
                + '</a>';
        }
    };
})();
