import { Routes } from '@angular/router';

export const routes: Routes = [
  { path: '', redirectTo: 'today', pathMatch: 'full' },
  {
    path: 'today',
    loadComponent: () =>
      import('./today/today.component').then((m) => m.TodayComponent),
  },
  {
    path: 'inbox',
    loadComponent: () =>
      import('./inbox/inbox.component').then((m) => m.InboxComponent),
  },
  {
    path: 'edit/:id',
    loadComponent: () =>
      import('./edit/edit.component').then((m) => m.EditComponent),
  },
];
