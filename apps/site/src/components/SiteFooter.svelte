<script lang="ts">
  import logo from '../assets/app-icon.webp';
  import {githubUrl} from '../../lib/site';
</script>

<footer class="site-footer">
  <a class="site-brand" href="/" aria-label="Polymux home"><img src={logo} alt="" /><span>Polymux</span></a>
  <p>Your agents. Your setup.</p>
  <nav aria-label="Footer navigation"><a href="/docs/">Docs</a><a href="/blog/">Blog</a><a href="/releases/">Releases</a><a href="/privacy-policy/">Privacy</a><a href={githubUrl}>GitHub</a></nav>
</footer>
