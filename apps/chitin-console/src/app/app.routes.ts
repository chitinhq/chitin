import { Route } from '@angular/router';

export const appRoutes: Route[] = [
  { path: '', pathMatch: 'full', redirectTo: 'overview' },
  {
    path: 'overview',
    loadComponent: () => import('./pages/overview.page').then(m => m.OverviewPage),
    data: { title: 'Overview' },
  },
  {
    path: 'sessions',
    loadComponent: () => import('./pages/sessions-list.page').then(m => m.SessionsListPage),
    data: { title: 'Sessions' },
  },
  {
    path: 'sessions/:chainId',
    loadComponent: () => import('./pages/session-detail.page').then(m => m.SessionDetailPage),
    data: { title: 'Session' },
  },
  {
    path: 'board',
    loadComponent: () => import('./pages/board.page').then(m => m.BoardPage),
    data: { title: 'Board' },
  },
  {
    path: 'tickets',
    loadComponent: () => import('./pages/tickets.page').then(m => m.TicketsPage),
    data: { title: 'Tickets' },
  },
  {
    path: 'elo',
    loadComponent: () => import('./pages/elo.page').then(m => m.EloPage),
    data: { title: 'Swarm ELO' },
  },
  {
    path: 'argus',
    loadComponent: () => import('./pages/argus.page').then(m => m.ArgusPage),
    data: { title: 'Argus' },
  },
  {
    path: 'reports',
    loadComponent: () => import('./pages/reports.page').then(m => m.ReportsPage),
    data: { title: 'Reports' },
  },
  {
    path: 'policy',
    loadComponent: () => import('./pages/policy.page').then(m => m.PolicyPage),
    data: { title: 'Policy' },
  },
  {
    path: 'suggestions',
    loadComponent: () => import('./pages/suggestions.page').then(m => m.SuggestionsPage),
    data: { title: 'Suggestions' },
  },
  { path: '**', redirectTo: 'overview' },
];
