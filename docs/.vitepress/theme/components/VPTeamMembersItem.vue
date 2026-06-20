<script setup lang="ts">
import type { DefaultTheme } from 'vitepress/theme'
import VPLink from 'vitepress/dist/client/theme-default/components/VPLink.vue'

interface Props {
  size?: 'small' | 'medium'
  member: DefaultTheme.TeamMember & {
    darkAvatar?: string
  }
}

withDefaults(defineProps<Props>(), {
  size: 'medium'
})
</script>

<template>
  <article class="VPTeamMembersItem" :class="[size]">
    <div class="profile">
      <figure class="avatar">
        <template v-if="member.darkAvatar">
          <img class="avatar-img dark" :src="member.darkAvatar" :alt="member.name" />
          <img class="avatar-img light" :src="member.avatar" :alt="member.name" />
        </template>
        <img v-else class="avatar-img" :src="member.avatar" :alt="member.name" />
      </figure>
      <div class="data">
        <h1 class="name">
          {{ member.name }}
        </h1>
      </div>
    </div>
  </article>
</template>

<style scoped>
.VPTeamMembersItem {
  display: flex;
  flex-direction: column;
  gap: 2px;
  border-radius: 12px;
  width: 100%;
  height: 100%;
  overflow: hidden;
}

.VPTeamMembersItem.small .profile {
  padding: 32px;
}

.VPTeamMembersItem.small .data {
  padding-top: 10px;
}

.VPTeamMembersItem.small .avatar {
  width: 64px;
  height: 64px;
}

.VPTeamMembersItem.small .name {
  line-height: 24px;
  font-size: 16px;
}

.VPTeamMembersItem.medium .profile {
  padding: 48px 32px;
}

.VPTeamMembersItem.medium .data {
  padding-top: 24px;
  text-align: center;
}

.VPTeamMembersItem.medium .avatar {
  width: 96px;
  height: 96px;
}

.VPTeamMembersItem.medium .name {
  letter-spacing: 0.15px;
  line-height: 28px;
  font-size: 20px;
}

.profile {
  flex-grow: 1;
  background-color: var(--vp-c-bg-soft);
}

.data {
  text-align: center;
  padding: 0;
}

.avatar {
  position: relative;
  flex-shrink: 0;
  margin: 0 auto;
}

.avatar-img {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  left: 0;
  object-fit: contain;
}

html:not(.dark) .avatar-img.dark {
  display: none;
}

.dark .avatar-img.light {
  display: none;
}

.name {
  margin: 0;
  font-weight: 600;
}
</style>
