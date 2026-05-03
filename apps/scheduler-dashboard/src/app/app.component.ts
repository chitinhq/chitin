import { Component } from '@angular/core';
import { RouterOutlet, RouterLink, RouterLinkActive } from '@angular/router';

@Component({
  selector: 'app-root',
  standalone: true,
  imports: [RouterOutlet, RouterLink, RouterLinkActive],
  styles: [`
    :host {
      display: flex;
      flex-direction: column;
      height: 100vh;
    }
    nav {
      display: flex;
      gap: 1.5rem;
      padding: 0.75rem 1.5rem;
      background: #1a1a2e;
      color: #fff;
      align-items: center;
    }
    nav h1 {
      margin: 0;
      font-size: 1.1rem;
      font-weight: 600;
      margin-right: auto;
      letter-spacing: 0.02em;
    }
    nav a {
      color: rgba(255,255,255,0.7);
      font-size: 0.9rem;
      padding: 0.35rem 0.75rem;
      border-radius: 4px;
      transition: background 0.15s, color 0.15s;
    }
    nav a:hover, nav a.active {
      background: rgba(255,255,255,0.12);
      color: #fff;
    }
    main {
      flex: 1;
      overflow-y: auto;
    }
  `],
  template: `
    <nav>
      <h1>Scheduler</h1>
      <a routerLink="/today" routerLinkActive="active">Today</a>
      <a routerLink="/inbox" routerLinkActive="active">Inbox</a>
    </nav>
    <main>
      <router-outlet />
    </main>
  `,
})
export class AppComponent {}
