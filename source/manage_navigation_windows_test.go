//go:build windows
package main
import "testing"
func TestEmptyChannelPaneDoesNotTrapUpDown(t *testing.T){
 g:=&nativeUI{width:1280,height:720,focus:1,groups:[]string{"Live TV","BD | News","BD | Bangla"},channels:[]channel{{Name:"News",Group:"BD | News",Type:"Live TV"},{Name:"Bangla",Group:"BD | Bangla",Type:"Live TV"}},archiveSection:"Live TV",groupIndex:1,visible:[]int{}}
 g.moveSelection(1)
 if g.focus!=0 || g.groupIndex!=2 {t.Fatalf("empty channel list swallowed Down: focus=%d group=%d",g.focus,g.groupIndex)}
 if len(g.visible)!=1 {t.Fatalf("group changed but channels did not reload: %d",len(g.visible))}
}
func TestLockedTopCategoryDoesNotShowFalseChannelCounts(t *testing.T){
 g:=&nativeUI{channels:[]channel{{Name:"News",Group:"BD | News",Type:"Live TV"},{Name:"Film",Group:"Hollywood",Type:"Movies"}}}
 g.manageCacheKey=g.managePlaylistKey()
 g.manageCache=map[string]bool{"k:Live TV":true}
 g.updateGroups()
 if g.groupCounts["Live TV"]!=0 {t.Fatalf("protected live media counted: %d",g.groupCounts["Live TV"])}
 for _,gr:=range g.groups {if gr=="BD | News" {t.Fatal("protected category remained visible")}}
}
