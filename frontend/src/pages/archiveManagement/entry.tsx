import React from 'react';
import './style.less';
const List = React.lazy(() => import('./List'));
const Detail = React.lazy(() => import('./Detail'));
const Document = React.lazy(() => import('./DocumentPage'));
export default {
  routes: [
    { path: '/archives', component: List, exact: true },
    { path: '/archives/:id/documents/:documentID', component: Document, exact: true },
    { path: '/archives/:id', component: Detail, exact: true },
  ],
};
