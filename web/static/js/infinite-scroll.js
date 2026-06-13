// ХозДача — общий хелпер бесконечной подгрузки через IntersectionObserver.
(function() {
    'use strict';

    function observe(sentinel, onLoadMore, options) {
        if (!sentinel || typeof onLoadMore !== 'function') return function() {};

        const root = (options && options.root) || null;
        const rootMargin = (options && options.rootMargin) || '400px 0px';
        const threshold = (options && options.threshold) != null ? options.threshold : 0;

        let busy = false;
        const io = new IntersectionObserver(function(entries) {
            entries.forEach(function(entry) {
                if (!entry.isIntersecting || busy) return;
                busy = true;
                Promise.resolve(onLoadMore()).finally(function() {
                    busy = false;
                });
            });
        }, { root: root, rootMargin: rootMargin, threshold: threshold });

        io.observe(sentinel);
        return function disconnect() { io.disconnect(); };
    }

    window.InfiniteScroll = { observe: observe };
})();
